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
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
)

// IAM Roles

type IAMRoles struct{}

func fetchRoleAndTags(svc *iam.IAM, roleName *string) (*iam.Role, Tags, error) {
	getRoleOutput, err := svc.GetRole(&iam.GetRoleInput{RoleName: roleName})
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
func roleIsManaged(role *iam.Role) bool {
	// Most AWS system roles are in a directory called `aws-service-role`
	if strings.HasPrefix(aws.StringValue(role.Path), "/aws-service-role/") {
		return false
	}

	return !builtinRoles.Has(aws.StringValue(role.RoleName))
}

func (IAMRoles) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := iam.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	var toDelete []*iamRole // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *iam.ListRolesOutput, _ bool) bool {
		for _, r := range page.Roles {
			if !roleIsManaged(r) {
				continue
			}
			role, tags, err := fetchRoleAndTags(svc, r.RoleName)
			if err != nil {
				logger.Warningf("failed fetching role and tags: %v", err)
				continue
			}
			l := &iamRole{arn: aws.StringValue(r.Arn), roleID: aws.StringValue(role.RoleId), roleName: aws.StringValue(role.RoleName)}
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

	if err := svc.ListRolesPages(&iam.ListRolesInput{}, pageFunc); err != nil {
		return err
	}

	for _, r := range toDelete {
		if err := r.delete(svc, logger); err != nil {
			logger.Warningf("%s: delete failed: %v", r.ARN(), err)
		}
	}

	return nil
}

func (IAMRoles) ListAll(opts Options) (*Set, error) {
	svc := iam.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(opts.DefaultTTL)
	inp := &iam.ListRolesInput{}

	err := svc.ListRolesPages(inp, func(roles *iam.ListRolesOutput, _ bool) bool {
		now := time.Now()
		for _, role := range roles.Roles {
			arn := iamRole{
				arn:      aws.StringValue(role.Arn),
				roleID:   aws.StringValue(role.RoleId),
				roleName: aws.StringValue(role.RoleName),
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

func (r iamRole) delete(svc *iam.IAM, logger logrus.FieldLogger) error {
	roleName := r.roleName

	var policyNames []string

	rolePoliciesReq := &iam.ListRolePoliciesInput{RoleName: aws.String(roleName)}
	err := svc.ListRolePoliciesPages(rolePoliciesReq, func(page *iam.ListRolePoliciesOutput, lastPage bool) bool {
		for _, policyName := range page.PolicyNames {
			policyNames = append(policyNames, aws.StringValue(policyName))
		}
		return true
	})

	if err != nil {
		return errors.Wrapf(err, "error listing IAM role policies for %q", roleName)
	}

	for _, policyName := range policyNames {
		logger.Infof("Deleting IAM role policy %q %q", roleName, policyName)

		deletePolicyReq := &iam.DeleteRolePolicyInput{
			RoleName:   aws.String(roleName),
			PolicyName: aws.String(policyName),
		}

		if _, err := svc.DeleteRolePolicy(deletePolicyReq); err != nil {
			return errors.Wrapf(err, "error deleting IAM role policy %q %q", roleName, policyName)
		}
	}

	logger.Debugf("Deleting IAM role %q", roleName)

	deleteReq := &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	}

	if _, err := svc.DeleteRole(deleteReq); err != nil {
		return errors.Wrapf(err, "error deleting IAM role %q", roleName)
	}

	return nil
}
