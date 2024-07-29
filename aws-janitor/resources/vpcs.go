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

// VPCs: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeVpcs

type VPCs struct{}

func (VPCs) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	resp, err := svc.DescribeVpcs(context.TODO(), &ec2v2.DescribeVpcsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws2.String("isDefault"),
				Values: []string{"false"},
			},
		},
	})
	if err != nil {
		return err
	}

	for _, vp := range resp.Vpcs {
		v := &vpc{Account: opts.Account, Region: opts.Region, ID: *vp.VpcId}
		tags := fromEC2Tags(vp.Tags)
		if !set.Mark(opts, v, nil, tags) {
			continue
		}
		logger.Warningf("%s: deleting %T: %s (%s)", v.ARN(), vp, v.ID, tags[NameTagKey])
		if opts.DryRun {
			continue
		}
		if vp.DhcpOptionsId != nil && *vp.DhcpOptionsId != "default" {
			disReq := &ec2v2.AssociateDhcpOptionsInput{
				VpcId:         vp.VpcId,
				DhcpOptionsId: aws2.String("default"),
			}

			if _, err := svc.AssociateDhcpOptions(context.TODO(), disReq); err != nil {
				logger.Warningf("%s: disassociating DHCP option set %s failed: %v", v.ARN(), *vp.DhcpOptionsId, err)
			}
		}

		if _, err := svc.DeleteVpc(context.TODO(), &ec2v2.DeleteVpcInput{VpcId: vp.VpcId}); err != nil {
			logger.Warningf("%s: delete failed: %v", v.ARN(), err)
		}
	}

	return nil
}

func (VPCs) ListAll(opts Options) (*Set, error) {
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	inp := &ec2v2.DescribeVpcsInput{}

	vpcs, err := svc.DescribeVpcs(context.TODO(), inp)
	if err != nil {
		return nil, errors.Wrapf(err, "couldn't describe VPCs for %q in %q", opts.Account, opts.Region)
	}

	now := time.Now()
	for _, v := range vpcs.Vpcs {
		arn := vpc{
			Account: opts.Account,
			Region:  opts.Region,
			ID:      *v.VpcId,
		}.ARN()
		set.firstSeen[arn] = now
	}

	return set, nil
}

type vpc struct {
	Account string
	Region  string
	ID      string
}

func (vp vpc) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:vpc/%s", vp.Region, vp.Account, vp.ID)
}

func (vp vpc) ResourceKey() string {
	return vp.ARN()
}
