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
	"time"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// SecurityGroups: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeSecurityGroups
type SecurityGroups struct{}

type sgRef struct {
	id   string
	perm *ec2types.IpPermission
}

func addRefs(refs map[string][]*sgRef, id string, account string, perms []ec2types.IpPermission) {
	for _, perm := range perms {
		perm := perm
		for _, pair := range perm.UserIdGroupPairs {
			pair := pair
			// Ignore cross-account for now, and skip circular refs.
			if *pair.UserId == account && *pair.GroupId != id {
				refs[*pair.GroupId] = append(refs[*pair.GroupId], &sgRef{id: id, perm: &perm})
			}
		}
	}
}

func (SecurityGroups) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	resp, err := svc.DescribeSecurityGroups(context.TODO(), nil)
	if err != nil {
		return err
	}

	var toDelete []*securityGroup        // Deferred to disentangle referencing security groups
	ingress := make(map[string][]*sgRef) // sg.GroupId -> [sg.GroupIds with this ingress]
	egress := make(map[string][]*sgRef)  // sg.GroupId -> [sg.GroupIds with this egress]
	for _, sg := range resp.SecurityGroups {
		if *sg.GroupName == "default" {
			// TODO(zmerlynn): Is there really no better way to detect this?
			continue
		}

		s := &securityGroup{Account: opts.Account, Region: opts.Region, ID: *sg.GroupId}
		addRefs(ingress, *sg.GroupId, opts.Account, sg.IpPermissions)
		addRefs(egress, *sg.GroupId, opts.Account, sg.IpPermissionsEgress)
		if !set.Mark(opts, s, nil, fromEC2Tags(sg.Tags)) {
			continue
		}
		logger.Warningf("%s: deleting %T: %s (%s)", s.ARN(), sg, s.ID, *sg.GroupName)
		if !opts.DryRun {
			toDelete = append(toDelete, s)
		}
	}

	for _, sg := range toDelete {

		// Revoke all ingress rules.
		for _, ref := range ingress[sg.ID] {
			logger.Infof("%s: revoking reference from %s", sg.ARN(), ref.id)

			revokeReq := &ec2v2.RevokeSecurityGroupIngressInput{
				GroupId:       &ref.id,
				IpPermissions: []ec2types.IpPermission{*ref.perm},
			}

			if _, err := svc.RevokeSecurityGroupIngress(context.TODO(), revokeReq); err != nil {
				logger.Warningf("%v: failed to revoke ingress reference from %s: %v", sg.ARN(), ref.id, err)
			}
		}

		// Revoke all egress rules.
		for _, ref := range egress[sg.ID] {
			logger.Infof("%s: revoking reference from %s", sg.ARN(), ref.id)

			revokeReq := &ec2v2.RevokeSecurityGroupEgressInput{
				GroupId:       aws2.String(ref.id),
				IpPermissions: []ec2types.IpPermission{*ref.perm},
			}

			if _, err := svc.RevokeSecurityGroupEgress(context.TODO(), revokeReq); err != nil {
				logger.Warningf("%s: failed to revoke egress reference from %s: %v", sg.ARN(), ref.id, err)
			}
		}

		// Delete security group.
		deleteReq := &ec2v2.DeleteSecurityGroupInput{
			GroupId: aws2.String(sg.ID),
		}

		if _, err := svc.DeleteSecurityGroup(context.TODO(), deleteReq); err != nil {
			logger.Warningf("%s: delete failed: %v", sg.ARN(), err)
		}
	}

	return nil
}

func (SecurityGroups) ListAll(opts Options) (*Set, error) {
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	input := &ec2v2.DescribeSecurityGroupsInput{}

	err := DescribeSecurityGroupsPages(svc, input, func(groups *ec2v2.DescribeSecurityGroupsOutput, _ bool) bool {
		now := time.Now()
		for _, sg := range groups.SecurityGroups {
			arn := securityGroup{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      *sg.GroupId,
			}.ARN()

			set.firstSeen[arn] = now
		}

		return true

	})

	return set, errors.Wrapf(err, "couldn't describe security groups for %q in %q", opts.Account, opts.Region)

}

func DescribeSecurityGroupsPages(svc *ec2v2.Client, input *ec2v2.DescribeSecurityGroupsInput, pageFunc func(groups *ec2v2.DescribeSecurityGroupsOutput, _ bool) bool) error {
	paginator := ec2v2.NewDescribeSecurityGroupsPaginator(svc, input)

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

type securityGroup struct {
	Account string
	Region  string
	ID      string
}

func (sg securityGroup) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:security-group/%s", sg.Region, sg.Account, sg.ID)
}

func (sg securityGroup) ResourceKey() string {
	return sg.ARN()
}
