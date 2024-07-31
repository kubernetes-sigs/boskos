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
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

// Clean-up Classic ELBs

type ClassicLoadBalancers struct{}

func (ClassicLoadBalancers) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := elbv2.NewFromConfig(*opts.Config, func(opt *elbv2.Options) {
		opt.Region = opts.Region
	})

	var loadBalancers []*classicLoadBalancer
	lbTags := make(map[string]Tags)

	pageFunc := func(page *elbv2.DescribeLoadBalancersOutput, _ bool) bool {
		for _, lb := range page.LoadBalancerDescriptions {
			clb := &classicLoadBalancer{
				region:      opts.Region,
				account:     opts.Account,
				name:        *lb.LoadBalancerName,
				dnsName:     *lb.DNSName,
				createdTime: lb.CreatedTime,
			}
			loadBalancers = append(loadBalancers, clb)
			lbTags[clb.name] = nil
		}
		return true
	}

	if err := DescribeLoadBalancersPages(svc, &elbv2.DescribeLoadBalancersInput{}, pageFunc); err != nil {
		return err
	}

	fetchTagErr := incrementalFetchTags(lbTags, 20, func(lbNames []*string) error {
		tagsResp, err := svc.DescribeTags(context.TODO(), &elbv2.DescribeTagsInput{LoadBalancerNames: aws2.ToStringSlice(lbNames)})
		if err != nil {
			return err
		}
		var errs []error
		for _, tagDesc := range tagsResp.TagDescriptions {
			lbName := *tagDesc.LoadBalancerName
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

		deleteInput := &elbv2.DeleteLoadBalancerInput{
			LoadBalancerName: &clb.name,
		}

		if _, err := svc.DeleteLoadBalancer(context.TODO(), deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", clb.ARN(), err)
		}
	}

	return nil
}

func (ClassicLoadBalancers) ListAll(opts Options) (*Set, error) {
	svc := elbv2.NewFromConfig(*opts.Config, func(opt *elbv2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	input := &elbv2.DescribeLoadBalancersInput{}

	err := DescribeLoadBalancersPages(svc, input, func(lbs *elbv2.DescribeLoadBalancersOutput, isLast bool) bool {
		now := time.Now()
		for _, lb := range lbs.LoadBalancerDescriptions {
			arn := classicLoadBalancer{
				region:  opts.Region,
				account: opts.Account,
				name:    *lb.LoadBalancerName,
				dnsName: *lb.DNSName,
			}.ARN()
			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe classic load balancers for %q in %q", opts.Account, opts.Region)
}

func DescribeLoadBalancersPages(svc *elbv2.Client, input *elbv2.DescribeLoadBalancersInput, pageFunc func(lbs *elbv2.DescribeLoadBalancersOutput, isLast bool) bool) error {
	paginator := elbv2.NewDescribeLoadBalancersPaginator(svc, input)

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
