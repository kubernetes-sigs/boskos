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
	"time"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	autoscalingv2 "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// AutoScalingGroups: https://docs.aws.amazon.com/sdk-for-go/api/service/autoscaling/#AutoScaling.DescribeAutoScalingGroups

type AutoScalingGroups struct{}

func (AutoScalingGroups) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := autoscalingv2.NewFromConfig(*opts.Config, func(opt *autoscalingv2.Options) {
		opt.Region = opts.Region
	})

	var toDelete []*autoScalingGroup // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *autoscalingv2.DescribeAutoScalingGroupsOutput, _ bool) bool {
		for _, asg := range page.AutoScalingGroups {
			a := &autoScalingGroup{
				arn:  *asg.AutoScalingGroupARN,
				name: *asg.AutoScalingGroupName,
			}
			tags := make(Tags, len(asg.Tags))
			for _, t := range asg.Tags {
				tags.Add(t.Key, t.Value)
			}
			if !set.Mark(opts, a, asg.CreatedTime, tags) {
				continue
			}

			logger.Warningf("%s: deleting %T: %s", a.ARN(), asg, a.name)
			if !opts.DryRun {
				toDelete = append(toDelete, a)
			}
		}
		return true
	}

	if err := DescribeAutoScalingGroupsPages(svc, &autoscalingv2.DescribeAutoScalingGroupsInput{}, pageFunc); err != nil {
		return err
	}

	for _, asg := range toDelete {
		deleteInput := &autoscalingv2.DeleteAutoScalingGroupInput{
			AutoScalingGroupName: aws2.String(asg.name),
			ForceDelete:          aws2.Bool(true),
		}

		if _, err := svc.DeleteAutoScalingGroup(context.TODO(), deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", asg.ARN(), err)
		}
	}

	// Block on ASGs finishing deletion. There are a lot of dependent
	// resources, so this just makes the rest go more smoothly (and
	// prevents a second pass).
	for _, asg := range toDelete {
		logger.Warningf("%s: waiting for delete", asg.ARN())
		waiter := autoscalingv2.NewGroupNotExistsWaiter(svc)
		if err := waiter.Wait(context.TODO(), &autoscalingv2.DescribeAutoScalingGroupsInput{
			AutoScalingGroupNames: []string{asg.name},
		}, 5*time.Minute); err != nil {
			logger.Warningf("%s: wait failed: %v", asg.ARN(), err)
		}
	}

	return nil
}

func DescribeAutoScalingGroupsPages(svc *autoscalingv2.Client, input *autoscalingv2.DescribeAutoScalingGroupsInput, pageFunc func(page *autoscalingv2.DescribeAutoScalingGroupsOutput, _ bool) bool) error {
	paginator := autoscalingv2.NewDescribeAutoScalingGroupsPaginator(svc, input)

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

func (AutoScalingGroups) ListAll(opts Options) (*Set, error) {
	svc := autoscalingv2.NewFromConfig(*opts.Config, func(opt *autoscalingv2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	input := &autoscalingv2.DescribeAutoScalingGroupsInput{}

	err := DescribeAutoScalingGroupsPages(svc, input, func(asgs *autoscalingv2.DescribeAutoScalingGroupsOutput, isLast bool) bool {
		now := time.Now()
		for _, asg := range asgs.AutoScalingGroups {
			arn := autoScalingGroup{
				arn:  *asg.AutoScalingGroupARN,
				name: *asg.AutoScalingGroupName,
			}.ARN()
			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe auto scaling groups for %q in %q", opts.Account, opts.Region)
}

type autoScalingGroup struct {
	arn  string
	name string
}

func (asg autoScalingGroup) ARN() string {
	return asg.arn
}

func (asg autoScalingGroup) ResourceKey() string {
	return asg.ARN()
}
