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
	"context"
	"fmt"
	"time"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	elbv2v2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

// Clean-up ELBs

type LoadBalancers struct{}

func (LoadBalancers) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := elbv2v2.NewFromConfig(*opts.Config, func(opt *elbv2v2.Options) {
		opt.Region = opts.Region
	})

	var loadBalancers []*loadBalancer
	lbTags := make(map[string]Tags)

	pageFunc := func(page *elbv2v2.DescribeLoadBalancersOutput, _ bool) bool {
		for _, lb := range page.LoadBalancers {
			a := &loadBalancer{
				arn:         *lb.LoadBalancerArn,
				name:        *lb.LoadBalancerName,
				createdTime: lb.CreatedTime,
			}
			loadBalancers = append(loadBalancers, a)
			lbTags[a.ARN()] = nil
		}
		return true
	}

	if err := DescribeLoadBalancersPagesv2(svc, &elbv2v2.DescribeLoadBalancersInput{}, pageFunc); err != nil {
		return err
	}

	fetchTagErr := incrementalFetchTags(lbTags, 20, func(lbArns []*string) error {
		tagsResp, err := svc.DescribeTags(context.TODO(), &elbv2v2.DescribeTagsInput{ResourceArns: aws2.ToStringSlice(lbArns)})
		if err != nil {
			return err
		}

		var errs []error
		for _, tagDesc := range tagsResp.TagDescriptions {
			arn := *tagDesc.ResourceArn
			_, ok := lbTags[arn]
			if !ok {
				errs = append(errs, fmt.Errorf("unknown load balancer ARN in tag response: %s", arn))
				continue
			}
			if lbTags[arn] == nil {
				lbTags[arn] = make(Tags, len(tagDesc.Tags))
			}
			for _, t := range tagDesc.Tags {
				lbTags[arn].Add(t.Key, t.Value)
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

		deleteInput := &elbv2v2.DeleteLoadBalancerInput{
			LoadBalancerArn: aws2.String(lb.ARN()),
		}

		if _, err := svc.DeleteLoadBalancer(context.TODO(), deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", lb.ARN(), err)
		}
	}

	return nil
}

func DescribeLoadBalancersPagesv2(svc *elbv2v2.Client, input *elbv2v2.DescribeLoadBalancersInput, pageFunc func(page *elbv2v2.DescribeLoadBalancersOutput, _ bool) bool) error {
	paginator := elbv2v2.NewDescribeLoadBalancersPaginator(svc, input)

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

func (LoadBalancers) ListAll(opts Options) (*Set, error) {
	svc := elbv2v2.NewFromConfig(*opts.Config, func(opt *elbv2v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	input := &elbv2v2.DescribeLoadBalancersInput{}

	err := DescribeLoadBalancersPagesv2(svc, input, func(lbs *elbv2v2.DescribeLoadBalancersOutput, isLast bool) bool {
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
