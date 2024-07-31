/*
Copyright 2022 The Kubernetes Authors.

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

	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// VPC endpoints: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeVpcEndpoints

type VPCEndpoints struct{}

func (VPCEndpoints) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	if !opts.EnableVPCEndpointsClean {
		logger.Info("Disable vpc endpoints clean")
		return nil
	}
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	var vpcEndPointsToDelete []*vpcEndpoint
	pageFunc := func(page *ec2v2.DescribeVpcEndpointsOutput, _ bool) bool {
		for _, vpce := range page.VpcEndpoints {
			v := &vpcEndpoint{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      *vpce.VpcEndpointId,
			}
			tags := fromEC2Tags(vpce.Tags)
			if !set.Mark(opts, v, nil, tags) {
				continue
			}
			logger.Warningf("%s: deleting %T: %s (%s)", v.ARN(), v, v.ID, tags[NameTagKey])
			if opts.DryRun {
				continue
			}
			vpcEndPointsToDelete = append(vpcEndPointsToDelete, v)
		}
		return true
	}

	if err := DescribeVpcEndpointsPages(svc, &ec2v2.DescribeVpcEndpointsInput{}, pageFunc); err != nil {
		return err
	}

	for _, v := range vpcEndPointsToDelete {
		if _, err := svc.DeleteVpcEndpoints(context.TODO(), &ec2v2.DeleteVpcEndpointsInput{VpcEndpointIds: []string{v.ID}}); err != nil {
			logger.Warningf("%s: delete failed: %v", v.ARN(), err)
		}
	}

	return nil
}

func DescribeVpcEndpointsPages(svc *ec2v2.Client, input *ec2v2.DescribeVpcEndpointsInput, pageFunc func(page *ec2v2.DescribeVpcEndpointsOutput, _ bool) bool) error {
	paginator := ec2v2.NewDescribeVpcEndpointsPaginator(svc, input)

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

func (VPCEndpoints) ListAll(opts Options) (*Set, error) {
	set := NewSet(0)
	if !opts.EnableVPCEndpointsClean {
		return set, nil
	}

	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	input := &ec2v2.DescribeVpcEndpointsInput{}
	err := DescribeVpcEndpointsPages(svc, input, func(page *ec2v2.DescribeVpcEndpointsOutput, _ bool) bool {
		now := time.Now()
		for _, vpce := range page.VpcEndpoints {
			arn := vpcEndpoint{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      *vpce.VpcEndpointId,
			}.ARN()
			set.firstSeen[arn] = now
		}
		return true
	})

	return set, errors.Wrapf(err, "couldn't describe vpc endpoints for %q in %q", opts.Account, opts.Region)
}

type vpcEndpoint struct {
	Account string
	Region  string
	ID      string
}

func (vp vpcEndpoint) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:vpcendpoint/%s", vp.Region, vp.Account, vp.ID)
}

func (vp vpcEndpoint) ResourceKey() string {
	return vp.ARN()
}
