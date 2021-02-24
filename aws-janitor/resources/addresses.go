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

// Elastic IPs: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeAddresses

type Addresses struct{}

func (Addresses) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	resp, err := svc.DescribeAddresses(nil)
	if err != nil {
		return err
	}

	for _, addr := range resp.Addresses {
		a := &address{Account: opts.Account, Region: opts.Region, ID: *addr.AllocationId}
		tags := fromEC2Tags(addr.Tags)
		if !set.Mark(opts, a, nil, tags) {
			continue
		}
		// Since tags and other metadata may not propagate to addresses from their associated instances,
		// we avoid deleting any address that is currently attached. Once their associated instance is deleted,
		// we can safely delete addresses in a later clean-up run (using the mark data we saved in this run).
		if addr.AssociationId != nil {
			continue
		}

		logger.Warningf("%s: deleting %T: %s (%s)", a.ARN(), addr, a.ID, tags[NameTagKey])
		if opts.DryRun {
			continue
		}
		_, err := svc.ReleaseAddress(&ec2.ReleaseAddressInput{AllocationId: addr.AllocationId})
		if err != nil {
			logger.Warningf("%s: delete failed: %v", a.ARN(), err)
		}
	}
	return nil
}

func (Addresses) ListAll(opts Options) (*Set, error) {
	svc := ec2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(0)
	inp := &ec2.DescribeAddressesInput{}

	addrs, err := svc.DescribeAddresses(inp)
	if err != nil {
		return nil, errors.Wrapf(err, "couldn't describe EC2 addresses for %q in %q", opts.Account, opts.Region)
	}

	now := time.Now()
	for _, addr := range addrs.Addresses {
		arn := address{
			Account: opts.Account,
			Region:  opts.Region,
			ID:      *addr.AllocationId,
		}.ARN()
		set.firstSeen[arn] = now
	}

	return set, nil
}

type address struct {
	Account string
	Region  string
	ID      string
}

func (addr address) ARN() string {
	// This ARN is a complete hallucination - there doesn't seem to be
	// an ARN for elastic IPs.
	return fmt.Sprintf("arn:aws:ec2:%s:%s:address/%s", addr.Region, addr.Account, addr.ID)
}

func (addr address) ResourceKey() string {
	return addr.ARN()
}
