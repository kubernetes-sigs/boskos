/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"context"
	"fmt"
	"strings"
	"time"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	iamv2 "github.com/aws/aws-sdk-go-v2/service/iam"
	iamv2types "github.com/aws/aws-sdk-go-v2/service/iam/types"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
)

// IAM Roles

type IAMRoles struct{}

func fetchRoleAndTags(svc *iamv2.Client, roleName *string) (*iamv2types.Role, Tags, error) {
	getRoleOutput, err := svc.GetRole(context.TODO(), &iamv2.GetRoleInput{RoleName: roleName})
	if err != nil {
		return nil, nil, err
	}
	if getRoleOutput.Role == nil {
		return nil, nil, fmt.Errorf("GetRole returned nil Role")
	}
	role := getRoleOutput.Role
	tags := make(Tags, len(role.Tags))
	for _, t := range role.Tags {
		tags.Add(t.Key, t.Value)
	}
	return role, tags, nil
}

// Roles defined by AWS documentation that do not live in the /aws-service-role/ path.
var builtinRoles = sets.NewString(
	// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-fleet-requests.html
	"aws-ec2-spot-fleet-tagging-role",
	// https://docs.aws.amazon.com/organizations/latest/userguide/orgs_manage_accounts_access.html
	"OrganizationAccountAccessRole",
)

// roleIsManaged checks if the role should be managed (and thus deleted) by us.
// In particular, we want to avoid "system" AWS roles.
// Note that this function does not consider tags.
func roleIsManaged(role iamv2types.Role) bool {
	// Most AWS system roles are in a directory called `aws-service-role`
	if strings.HasPrefix(*role.Path, "/aws-service-role/") {
		return false
	}

	return !builtinRoles.Has(*role.RoleName)
}

func (IAMRoles) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := iamv2.NewFromConfig(*opts.Config, func(opt *iamv2.Options) {
		opt.Region = opts.Region
	})

	var toDelete []*iamRole // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *iamv2.ListRolesOutput, _ bool) bool {
		for _, r := range page.Roles {
			if !roleIsManaged(r) {
				continue
			}
			role, tags, err := fetchRoleAndTags(svc, r.RoleName)
			if err != nil {
				logger.Warningf("failed fetching role and tags: %v", err)
				continue
			}
			l := &iamRole{arn: *r.Arn, roleID: *role.RoleId, roleName: *role.RoleName}
			if set.Mark(opts, l, r.CreateDate, tags) {
				if role.RoleLastUsed != nil && role.RoleLastUsed.LastUsedDate != nil && time.Since(*role.RoleLastUsed.LastUsedDate) < set.ttl {
					logger.Debugf("%s: used too recently, skipping", l.ARN())
					continue
				}
				logger.Warningf("%s: deleting %T: %s", l.ARN(), role, l.roleName)
				if !opts.DryRun {
					toDelete = append(toDelete, l)
				}
			}
		}
		return true
	}

	if err := ListRolesPages(svc, &iamv2.ListRolesInput{}, pageFunc); err != nil {
		return err
	}

	for _, r := range toDelete {
		if err := r.delete(svc, logger); err != nil {
			logger.Warningf("%s: delete failed: %v", r.ARN(), err)
		}
	}

	return nil
}

func ListRolesPages(svc *iamv2.Client, input *iamv2.ListRolesInput, pageFunc func(page *iamv2.ListRolesOutput, _ bool) bool) error {
	paginator := iamv2.NewListRolesPaginator(svc, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			logrus.Warningf("failed to get page, %v", err)
		} else {
			pageFunc(page, false)
		}
	}
	return nil
}

func (IAMRoles) ListAll(opts Options) (*Set, error) {
	svc := iamv2.NewFromConfig(*opts.Config, func(opt *iamv2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	inp := &iamv2.ListRolesInput{}

	err := ListRolesPages(svc, inp, func(roles *iamv2.ListRolesOutput, _ bool) bool {
		now := time.Now()
		for _, role := range roles.Roles {
			arn := iamRole{
				arn:      *role.Arn,
				roleID:   *role.RoleId,
				roleName: *role.RoleName,
			}.ARN()

			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe iam roles for %q in %q", opts.Account, opts.Region)
}

type iamRole struct {
	arn      string
	roleID   string
	roleName string
}

func (r iamRole) ARN() string {
	return r.arn
}

func (r iamRole) ResourceKey() string {
	return r.roleID + "::" + r.ARN()
}

func (r iamRole) delete(svc *iamv2.Client, logger logrus.FieldLogger) error {
	roleName := r.roleName

	var policyNames []string

	rolePoliciesReq := &iamv2.ListRolePoliciesInput{RoleName: aws2.String(roleName)}
	err := ListRolePoliciesPages(svc, rolePoliciesReq, func(page *iamv2.ListRolePoliciesOutput, lastPage bool) bool {
		for _, policyName := range page.PolicyNames {
			policyNames = append(policyNames, policyName)
		}
		return true
	})

	if err != nil {
		return errors.Wrapf(err, "error listing IAM role policies for %q", roleName)
	}

	var attachedRolePolicyArns []string
	attachedRolePoliciesReq := &iamv2.ListAttachedRolePoliciesInput{RoleName: aws2.String(roleName)}
	err = ListAttachedRolePoliciesPages(svc, attachedRolePoliciesReq, func(page *iamv2.ListAttachedRolePoliciesOutput, _ bool) bool {
		for _, attachedRolePolicy := range page.AttachedPolicies {
			attachedRolePolicyArns = append(attachedRolePolicyArns, *attachedRolePolicy.PolicyArn)
		}
		return true
	})

	if err != nil {
		return errors.Wrapf(err, "error listing IAM attached role policies for %q", roleName)
	}

	for _, policyName := range policyNames {
		logger.Infof("Deleting IAM role policy %q %q", roleName, policyName)

		deletePolicyReq := &iamv2.DeleteRolePolicyInput{
			RoleName:   aws2.String(roleName),
			PolicyName: aws2.String(policyName),
		}

		if _, err := svc.DeleteRolePolicy(context.TODO(), deletePolicyReq); err != nil {
			return errors.Wrapf(err, "error deleting IAM role policy %q %q", roleName, policyName)
		}
	}

	for _, attachedRolePolicyArn := range attachedRolePolicyArns {
		logger.Infof("Detaching IAM attached role policy %q %q", roleName, attachedRolePolicyArn)

		detachRolePolicyReq := &iamv2.DetachRolePolicyInput{
			PolicyArn: aws2.String(attachedRolePolicyArn),
			RoleName:  aws2.String(roleName),
		}

		if _, err := svc.DetachRolePolicy(context.TODO(), detachRolePolicyReq); err != nil {
			return errors.Wrapf(err, "error detaching IAM role policy %q %q", roleName, attachedRolePolicyArn)
		}
	}

	logger.Debugf("Deleting IAM role %q", roleName)

	deleteReq := &iamv2.DeleteRoleInput{
		RoleName: aws2.String(roleName),
	}

	if _, err := svc.DeleteRole(context.TODO(), deleteReq); err != nil {
		return errors.Wrapf(err, "error deleting IAM role %q", roleName)
	}

	return nil
}

func ListRolePoliciesPages(svc *iamv2.Client, input *iamv2.ListRolePoliciesInput, pageFunc func(page *iamv2.ListRolePoliciesOutput, lastPage bool) bool) error {
	paginator := iamv2.NewListRolePoliciesPaginator(svc, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			logrus.Warningf("failed to get page, %v", err)
		} else {
			pageFunc(page, false)
		}
	}
	return nil
}

func ListAttachedRolePoliciesPages(svc *iamv2.Client, input *iamv2.ListAttachedRolePoliciesInput, pageFunc func(page *iamv2.ListAttachedRolePoliciesOutput, lastPage bool) bool) error {
	paginator := iamv2.NewListAttachedRolePoliciesPaginator(svc, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			logrus.Warningf("failed to get page, %v", err)
		} else {
			pageFunc(page, false)
		}
	}
	return nil
}
