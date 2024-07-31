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

// InternetGateways: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeInternetGateways

type InternetGateways struct{}

func (InternetGateways) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	resp, err := svc.DescribeInternetGateways(context.TODO(), nil)
	if err != nil {
		return err
	}

	vpcResp, err := svc.DescribeVpcs(context.TODO(), &ec2v2.DescribeVpcsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws2.String("isDefault"),
				Values: []string{"true"},
			},
		},
	})

	if err != nil {
		return err
	}

	// Use a map to tolerate both more than one default vpc
	// (shouldn't happen) as well as no default VPC (not uncommon)
	defaultVPC := make(map[string]bool)
	for _, vpc := range vpcResp.Vpcs {
		defaultVPC[*vpc.VpcId] = true
	}

	for _, ig := range resp.InternetGateways {
		i := &internetGateway{Account: opts.Account, Region: opts.Region, ID: *ig.InternetGatewayId}
		tags := fromEC2Tags(ig.Tags)
		if !set.Mark(opts, i, nil, tags) {
			continue
		}
		logger.Warningf("%s: deleting %T: %s (%s)", i.ARN(), ig, i.ID, tags[NameTagKey])
		if opts.DryRun {
			continue
		}

		isDefault := false
		for _, att := range ig.Attachments {
			if defaultVPC[*att.VpcId] {
				isDefault = true
				break
			}

			detachReq := &ec2v2.DetachInternetGatewayInput{
				InternetGatewayId: ig.InternetGatewayId,
				VpcId:             att.VpcId,
			}

			if _, err := svc.DetachInternetGateway(context.TODO(), detachReq); err != nil {
				logger.Warningf("%s: detach from %s failed: %v", i.ARN(), *att.VpcId, err)
			}
		}

		if isDefault {
			logger.Infof("%s: skipping delete as IGW is the default for the VPC %T: %s", i.ARN(), ig, i.ID)
			continue
		}

		deleteReq := &ec2v2.DeleteInternetGatewayInput{
			InternetGatewayId: ig.InternetGatewayId,
		}

		if _, err := svc.DeleteInternetGateway(context.TODO(), deleteReq); err != nil {
			logger.Warningf("%s: delete failed: %v", i.ARN(), err)
		}
	}

	return nil
}

func (InternetGateways) ListAll(opts Options) (*Set, error) {
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	input := &ec2v2.DescribeInternetGatewaysInput{}

	gateways, err := svc.DescribeInternetGateways(context.TODO(), input)
	if err != nil {
		return set, errors.Wrapf(err, "couldn't describe internet gateways for %q in %q", opts.Account, opts.Region)
	}
	now := time.Now()
	for _, gateway := range gateways.InternetGateways {
		arn := internetGateway{
			Account: opts.Account,
			Region:  opts.Region,
			ID:      *gateway.InternetGatewayId,
		}.ARN()
		set.firstSeen[arn] = now
	}

	return set, nil
}

type internetGateway struct {
	Account string
	Region  string
	ID      string
}

func (ig internetGateway) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:internet-gateway/%s", ig.Region, ig.Account, ig.ID)
}

func (ig internetGateway) ResourceKey() string {
	return ig.ARN()
}
