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
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Clean-up Classic ELBs

type ClassicLoadBalancers struct{}

func (ClassicLoadBalancers) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := elb.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	var toDelete []*classicLoadBalancer // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *elb.DescribeLoadBalancersOutput, _ bool) bool {
		for _, lb := range page.LoadBalancerDescriptions {
			a := &classicLoadBalancer{
				region:  opts.Region,
				account: opts.Account,
				name:    aws.StringValue(lb.LoadBalancerName),
				dnsName: aws.StringValue(lb.DNSName),
			}
			if set.Mark(a, lb.CreatedTime) {
				logger.Warningf("%s: deleting %T: %s", a.ARN(), lb, a.name)
				if !opts.DryRun {
					toDelete = append(toDelete, a)
				}
			}
		}
		return true
	}

	if err := svc.DescribeLoadBalancersPages(&elb.DescribeLoadBalancersInput{}, pageFunc); err != nil {
		return err
	}

	for _, lb := range toDelete {
		deleteInput := &elb.DeleteLoadBalancerInput{
			LoadBalancerName: aws.String(lb.name),
		}

		if _, err := svc.DeleteLoadBalancer(deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", lb.ARN(), err)
		}
	}

	return nil
}

func (ClassicLoadBalancers) ListAll(opts Options) (*Set, error) {
	c := elb.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(0)
	input := &elb.DescribeLoadBalancersInput{}

	err := c.DescribeLoadBalancersPages(input, func(lbs *elb.DescribeLoadBalancersOutput, isLast bool) bool {
		now := time.Now()
		for _, lb := range lbs.LoadBalancerDescriptions {
			arn := classicLoadBalancer{
				region:  opts.Region,
				account: opts.Account,
				name:    aws.StringValue(lb.LoadBalancerName),
				dnsName: aws.StringValue(lb.DNSName),
			}.ARN()
			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe classic load balancers for %q in %q", opts.Account, opts.Region)
}

type classicLoadBalancer struct {
	region  string
	account string
	name    string
	dnsName string
}

func (lb classicLoadBalancer) ARN() string {
	return fmt.Sprintf("arn:aws:elb:%s:%s:classicelb/%s", lb.region, lb.account, lb.dnsName)
}

func (lb classicLoadBalancer) ResourceKey() string {
	return lb.ARN()
}
