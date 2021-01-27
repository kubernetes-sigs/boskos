/*
Copyright 2020 The Kubernetes Authors.

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
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

// Clean-up ELBs

type LoadBalancers struct{}

func (LoadBalancers) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := elbv2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	var loadBalancers []*loadBalancer
	lbTags := make(map[string][]Tag)

	pageFunc := func(page *elbv2.DescribeLoadBalancersOutput, _ bool) bool {
		for _, lb := range page.LoadBalancers {
			a := &loadBalancer{
				arn:         aws.StringValue(lb.LoadBalancerArn),
				name:        aws.StringValue(lb.LoadBalancerName),
				createdTime: lb.CreatedTime,
			}
			loadBalancers = append(loadBalancers, a)
			lbTags[a.ARN()] = nil
		}
		return true
	}

	if err := svc.DescribeLoadBalancersPages(&elbv2.DescribeLoadBalancersInput{}, pageFunc); err != nil {
		return err
	}

	fetchTagErr := incrementalFetchTags(lbTags, 20, func(lbArns []*string) error {
		tagsResp, err := svc.DescribeTags(&elbv2.DescribeTagsInput{ResourceArns: lbArns})
		if err != nil {
			return err
		}

		var errs []error
		for _, tagDesc := range tagsResp.TagDescriptions {
			arn := aws.StringValue(tagDesc.ResourceArn)
			_, ok := lbTags[arn]
			if !ok {
				errs = append(errs, fmt.Errorf("unknown load balancer ARN in tag response: %s", arn))
				continue
			}
			for _, t := range tagDesc.Tags {
				lbTags[arn] = append(lbTags[arn], NewTag(t.Key, t.Value))
			}
		}
		return kerrors.NewAggregate(errs)
	})

	if fetchTagErr != nil {
		return fetchTagErr
	}

	for _, lb := range loadBalancers {
		if !set.Mark(opts, lb, lb.createdTime, lbTags[lb.ARN()]) {
			continue
		}
		logger.Warningf("%s: deleting %T: %s", lb.ARN(), lb, lb.name)

		if opts.DryRun {
			continue
		}

		deleteInput := &elbv2.DeleteLoadBalancerInput{
			LoadBalancerArn: aws.String(lb.ARN()),
		}

		if _, err := svc.DeleteLoadBalancer(deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", lb.ARN(), err)
		}
	}

	return nil
}

func (LoadBalancers) ListAll(opts Options) (*Set, error) {
	c := elbv2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(0)
	input := &elbv2.DescribeLoadBalancersInput{}

	err := c.DescribeLoadBalancersPages(input, func(lbs *elbv2.DescribeLoadBalancersOutput, isLast bool) bool {
		now := time.Now()
		for _, lb := range lbs.LoadBalancers {
			a := &loadBalancer{arn: *lb.LoadBalancerArn, name: *lb.LoadBalancerName}
			set.firstSeen[a.ResourceKey()] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe load balancers for %q in %q", opts.Account, opts.Region)
}

type loadBalancer struct {
	arn         string
	name        string
	createdTime *time.Time
}

func (lb loadBalancer) ARN() string {
	return lb.arn
}

func (lb loadBalancer) ResourceKey() string {
	return lb.ARN()
}
