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
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// InternetGateways: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeInternetGateways

type InternetGateways struct{}

func (InternetGateways) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	resp, err := svc.DescribeInternetGateways(nil)
	if err != nil {
		return err
	}

	vpcResp, err := svc.DescribeVpcs(&ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("isDefault"),
				Values: []*string{aws.String("true")},
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
		defaultVPC[aws.StringValue(vpc.VpcId)] = true
	}

	for _, ig := range resp.InternetGateways {
		i := &internetGateway{Account: opts.Account, Region: opts.Region, ID: *ig.InternetGatewayId}
		tags := fromEC2Tags(ig.Tags)
		if !set.Mark(opts, i, nil, tags) {
			continue
		}
		logger.Warningf("%s: deleting %T: %s (%s)", i.ARN(), ig, i.ID, tags[NameTagKey])

		isDefault := false
		for _, att := range ig.Attachments {
			if defaultVPC[aws.StringValue(att.VpcId)] {
				isDefault = true
				break
			}

			if opts.DisassociatePublicIP {
				var publicIPsToRelease []*string

				pageFunc := func(page *ec2.DescribeNetworkInterfacesOutput, _ bool) bool {
					for _, eni := range page.NetworkInterfaces {
						publicIPsToRelease = append(publicIPsToRelease, eni.Association.PublicIp)
					}
					return true
				}

				if err := svc.DescribeNetworkInterfacesPages(&ec2.DescribeNetworkInterfacesInput{
					Filters: []*ec2.Filter{
						{
							Name:   aws.String("vpc-id"),
							Values: []*string{aws.String(*att.VpcId)},
						},
					},
				}, pageFunc); err != nil {
					logger.Warningf("fail to get public IP for vpc %s: %v", *att.VpcId, err)
				}

				logger.Warningf("%s: disassociating public IPs for vpc: %s", i.ARN(), *att.VpcId)
				if opts.DryRun {
					continue
				}
				// According to https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Internet_Gateway.html#detach-igw
				// Before detaching the internet gateway, we must dissassociate elastic IPs first.
				for _, publicIP := range publicIPsToRelease {
					disassociateReq := &ec2.DisassociateAddressInput{
						PublicIp: aws.String(*publicIP),
					}
					if _, err := svc.DisassociateAddress(disassociateReq); err != nil {
						logger.Warningf("%s: disassociate failed: %v", *publicIP, err)
					}
				}
			}
			logger.Warningf("%s: detaching internet gateway: %s for vpc: %s", i.ARN(), *ig.InternetGatewayId, *att.VpcId)
			if opts.DryRun {
				continue
			}

			detachReq := &ec2.DetachInternetGatewayInput{
				InternetGatewayId: ig.InternetGatewayId,
				VpcId:             att.VpcId,
			}

			if _, err := svc.DetachInternetGateway(detachReq); err != nil {
				logger.Warningf("%s: detach from %s failed: %v", i.ARN(), *att.VpcId, err)
			}
		}

		if isDefault {
			logger.Infof("%s: skipping delete as IGW is the default for the VPC %T: %s", i.ARN(), ig, i.ID)
			continue
		}

		if opts.DryRun {
			continue
		}

		deleteReq := &ec2.DeleteInternetGatewayInput{
			InternetGatewayId: ig.InternetGatewayId,
		}

		if _, err := svc.DeleteInternetGateway(deleteReq); err != nil {
			logger.Warningf("%s: delete failed: %v", i.ARN(), err)
		}
	}

	return nil
}

func (InternetGateways) ListAll(opts Options) (*Set, error) {
	svc := ec2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(0)
	input := &ec2.DescribeInternetGatewaysInput{}

	gateways, err := svc.DescribeInternetGateways(input)
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
