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
	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// DHCPOptions: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeDhcpOptions
type DHCPOptions struct{}

func (DHCPOptions) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	// This is a little gross, but I can't find an easier way to
	// figure out the DhcpOptions associated with the default VPC.
	defaultRefs := make(map[string]bool)
	{
		resp, err := svc.DescribeVpcs(context.TODO(), &ec2v2.DescribeVpcsInput{
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

		for _, vpc := range resp.Vpcs {
			defaultRefs[*vpc.DhcpOptionsId] = true
		}
	}

	resp, err := svc.DescribeDhcpOptions(context.TODO(), nil)
	if err != nil {
		return err
	}

	var defaults []string
	for _, dhcp := range resp.DhcpOptions {
		if defaultRefs[*dhcp.DhcpOptionsId] {
			continue
		}

		// Separately, skip any "default looking" DHCP Option Sets. See comment below.
		if defaultLookingDHCPOptions(dhcp, opts.Region) {
			defaults = append(defaults, *dhcp.DhcpOptionsId)
			continue
		}

		dh := &dhcpOption{Account: opts.Account, Region: opts.Region, ID: *dhcp.DhcpOptionsId}
		tags := fromEC2Tags(dhcp.Tags)
		if !set.Mark(opts, dh, nil, tags) {
			continue
		}

		logger.Warningf("%s: deleting %T: %s (%s)", dh.ARN(), dhcp, dh.ID, tags[NameTagKey])
		if opts.DryRun {
			continue
		}

		if _, err := svc.DeleteDhcpOptions(context.TODO(), &ec2v2.DeleteDhcpOptionsInput{DhcpOptionsId: dhcp.DhcpOptionsId}); err != nil {
			logger.Warningf("%s: delete failed: %v", dh.ARN(), err)
		}
	}

	if len(defaults) > 1 {
		logger.Errorf("Found more than one default-looking DHCP option set: %s", strings.Join(defaults, ", "))
	}

	return nil
}

func (DHCPOptions) ListAll(opts Options) (*Set, error) {
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	inp := &ec2v2.DescribeDhcpOptionsInput{}

	optsList, err := svc.DescribeDhcpOptions(context.TODO(), inp)
	if err != nil {
		return nil, errors.Wrapf(err, "couldn't describe DHCP Options for %q in %q", opts.Account, opts.Region)
	}

	now := time.Now()
	for _, dhcpOpts := range optsList.DhcpOptions {
		arn := dhcpOption{
			Account: opts.Account,
			Region:  opts.Region,
			ID:      *dhcpOpts.DhcpOptionsId,
		}.ARN()
		set.firstSeen[arn] = now
	}

	return set, nil
}

// defaultLookingDHCPOptions: This part is a little annoying. If
// you're running in a region with where there is no default-looking
// DHCP option set, when you create any VPC, AWS will create a
// default-looking DHCP option set for you. If you then re-associate
// or delete the VPC, the option set will hang around. However, if you
// have a default-looking DHCP option set (even with no default VPC)
// and create a VPC, AWS will associate the VPC with the DHCP option
// set of the default VPC. There's no signal as to whether the option
// set returned is the default or was created along with the
// VPC. Because of this, we just skip these during cleanup - there
// will only ever be one default set per region.
func defaultLookingDHCPOptions(dhcp ec2types.DhcpOptions, region string) bool {
	if len(dhcp.Tags) != 0 {
		return false
	}

	for _, conf := range dhcp.DhcpConfigurations {
		switch *conf.Key {
		case "domain-name":
			var domain string
			// TODO(akutz): Should this be updated to regions.Default, or is
			// this relying on the default region for EC2 for North America?
			// Because EC2's default region changed from us-east-1 to us-east-2
			// depending on when the account was created.
			if region == "us-east-1" {
				domain = "ec2.internal"
			} else {
				domain = region + ".compute.internal"
			}

			// TODO(vincepri): Investigate this line, seems it might segfault if conf.Values is 0?
			if len(conf.Values) != 1 || *conf.Values[0].Value != domain {
				return false
			}
		case "domain-name-servers":
			// TODO(vincepri): Same as above.
			if len(conf.Values) != 1 || *conf.Values[0].Value != "AmazonProvidedDNS" {
				return false
			}
		default:
			return false
		}
	}

	return true
}

type dhcpOption struct {
	Account string
	Region  string
	ID      string
}

func (dhcp dhcpOption) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:dhcp-option/%s", dhcp.Region, dhcp.Account, dhcp.ID)
}

func (dhcp dhcpOption) ResourceKey() string {
	return dhcp.ARN()
}
