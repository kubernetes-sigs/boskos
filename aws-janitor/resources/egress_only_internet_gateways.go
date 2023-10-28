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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// EgressOnlyInternetGateways: https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_DescribeEgressOnlyInternetGateways.html

// EgressOnlyInternetGateways allow for IPv6 egress: https://docs.aws.amazon.com/vpc/latest/userguide/egress-only-internet-gateway.html
type EgressOnlyInternetGateways struct{}

// MarkAndSweep looks at the provided set, and removes resources older than its TTL that have been previously tagged.
func (EgressOnlyInternetGateways) MarkAndSweep(opts Options, set *Set) error {
	ctx := context.TODO()

	logger := logrus.WithField("options", opts)
	svc := ec2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	inp := &ec2.DescribeEgressOnlyInternetGatewaysInput{}
	if err := svc.DescribeEgressOnlyInternetGatewaysPagesWithContext(ctx, inp, func(page *ec2.DescribeEgressOnlyInternetGatewaysOutput, _ bool) bool {
		for _, gw := range page.EgressOnlyInternetGateways {
			g := &egressOnlyInternetGateway{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      aws.StringValue(gw.EgressOnlyInternetGatewayId),
			}

			tags := fromEC2Tags(gw.Tags)
			if !set.Mark(opts, g, nil, tags) {
				continue
			}
			logger.Warningf("%s: deleting %T: %s (%s)", g.ResourceKey(), gw, g.ID, tags[NameTagKey])
			if opts.DryRun {
				continue
			}
			inp := &ec2.DeleteEgressOnlyInternetGatewayInput{EgressOnlyInternetGatewayId: gw.EgressOnlyInternetGatewayId}
			if _, err := svc.DeleteEgressOnlyInternetGatewayWithContext(ctx, inp); err != nil {
				logger.Warningf("%s: delete failed: %v", g.ARN(), err)
			}
		}
		return true
	}); err != nil {
		return err
	}

	return nil
}

// ListAll populates a set with all available EgressOnlyInternetGateway resources.
func (EgressOnlyInternetGateways) ListAll(opts Options) (*Set, error) {
	ctx := context.TODO()

	svc := ec2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(0)

	inp := &ec2.DescribeEgressOnlyInternetGatewaysInput{}
	err := svc.DescribeEgressOnlyInternetGatewaysPagesWithContext(ctx, inp, func(page *ec2.DescribeEgressOnlyInternetGatewaysOutput, _ bool) bool {
		for _, gw := range page.EgressOnlyInternetGateways {
			now := time.Now()
			arn := natGateway{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      *gw.EgressOnlyInternetGatewayId,
			}.ARN()

			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe egressOnlyInternetGateways for %q in %q", opts.Account, opts.Region)
}

type egressOnlyInternetGateway struct {
	Account string
	Region  string
	ID      string
}

func (ng egressOnlyInternetGateway) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:egressOnlyInternetGateway/%s", ng.Region, ng.Account, ng.ID)
}

func (ng egressOnlyInternetGateway) ResourceKey() string {
	return ng.ARN()
}
