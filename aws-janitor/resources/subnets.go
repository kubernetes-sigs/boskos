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

// Subnets: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeSubnets
type Subnets struct{}

func (Subnets) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	descReq := &ec2v2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws2.String("defaultForAz"),
				Values: []string{"false"},
			},
		},
	}

	resp, err := svc.DescribeSubnets(context.TODO(), descReq)
	if err != nil {
		return err
	}

	for _, sub := range resp.Subnets {
		s := &subnet{Account: opts.Account, Region: opts.Region, ID: *sub.SubnetId}
		tags := fromEC2Tags(sub.Tags)
		if !set.Mark(opts, s, nil, tags) {
			continue
		}

		logger.Warningf("%s: deleting %T: %s (%s)", s.ARN(), sub, s.ID, tags[NameTagKey])
		if opts.DryRun {
			continue
		}
		if _, err := svc.DeleteSubnet(context.TODO(), &ec2v2.DeleteSubnetInput{SubnetId: sub.SubnetId}); err != nil {
			logger.Warningf("%s: delete failed: %v", s.ARN(), err)
		}
	}

	return nil
}

func (Subnets) ListAll(opts Options) (*Set, error) {
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	input := &ec2v2.DescribeSubnetsInput{}

	// Subnets not paginated
	subnets, err := svc.DescribeSubnets(context.TODO(), input)
	now := time.Now()
	for _, sn := range subnets.Subnets {
		arn := subnet{
			Account: opts.Account,
			Region:  opts.Region,
			ID:      *sn.SubnetId,
		}.ARN()
		set.firstSeen[arn] = now
	}

	return set, errors.Wrapf(err, "couldn't describe subnets for %q in %q", opts.Account, opts.Region)
}

type subnet struct {
	Account string
	Region  string
	ID      string
}

func (sub subnet) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:subnet/%s", sub.Region, sub.Account, sub.ID)
}

func (sub subnet) ResourceKey() string {
	return sub.ARN()
}
