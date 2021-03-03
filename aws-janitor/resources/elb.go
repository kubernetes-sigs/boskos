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
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

// Clean-up Classic ELBs

type ClassicLoadBalancers struct{}

func (ClassicLoadBalancers) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := elb.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	var loadBalancers []*classicLoadBalancer
	lbTags := make(map[string]Tags)

	pageFunc := func(page *elb.DescribeLoadBalancersOutput, _ bool) bool {
		for _, lb := range page.LoadBalancerDescriptions {
			clb := &classicLoadBalancer{
				region:      opts.Region,
				account:     opts.Account,
				name:        aws.StringValue(lb.LoadBalancerName),
				dnsName:     aws.StringValue(lb.DNSName),
				createdTime: lb.CreatedTime,
			}
			loadBalancers = append(loadBalancers, clb)
			lbTags[clb.name] = nil
		}
		return true
	}

	if err := svc.DescribeLoadBalancersPages(&elb.DescribeLoadBalancersInput{}, pageFunc); err != nil {
		return err
	}

	fetchTagErr := incrementalFetchTags(lbTags, 20, func(lbNames []*string) error {
		tagsResp, err := svc.DescribeTags(&elb.DescribeTagsInput{LoadBalancerNames: lbNames})
		if err != nil {
			return err
		}
		var errs []error
		for _, tagDesc := range tagsResp.TagDescriptions {
			lbName := aws.StringValue(tagDesc.LoadBalancerName)
			_, ok := lbTags[lbName]
			if !ok {
				errs = append(errs, fmt.Errorf("unknown load balancer in tag response: %s", lbName))
				continue
			}
			if lbTags[lbName] == nil {
				lbTags[lbName] = make(Tags, len(tagDesc.Tags))
			}
			for _, t := range tagDesc.Tags {
				lbTags[lbName].Add(t.Key, t.Value)
			}
		}
		return kerrors.NewAggregate(errs)
	})

	if fetchTagErr != nil {
		return fetchTagErr
	}

	for _, clb := range loadBalancers {
		if !set.Mark(opts, clb, clb.createdTime, lbTags[clb.name]) {
			continue
		}
		logger.Warningf("%s: deleting %T: %s", clb.ARN(), clb, clb.name)

		if opts.DryRun {
			continue
		}

		deleteInput := &elb.DeleteLoadBalancerInput{
			LoadBalancerName: aws.String(clb.name),
		}

		if _, err := svc.DeleteLoadBalancer(deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", clb.ARN(), err)
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
	region      string
	account     string
	name        string
	dnsName     string
	createdTime *time.Time
}

func (lb classicLoadBalancer) ARN() string {
	return fmt.Sprintf("arn:aws:elb:%s:%s:classicelb/%s", lb.region, lb.account, lb.dnsName)
}

func (lb classicLoadBalancer) ResourceKey() string {
	return lb.ARN()
}
