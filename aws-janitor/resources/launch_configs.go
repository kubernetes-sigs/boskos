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

// LaunchConfigurations: http://docs.aws.amazon.com/sdk-for-go/api/service/autoscaling/#AutoScaling.DescribeLaunchConfigurations
type LaunchConfigurations struct{}

func (LaunchConfigurations) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := autoscalingv2.NewFromConfig(*opts.Config, func(opt *autoscalingv2.Options) {
		opt.Region = opts.Region
	})

	var toDelete []*launchConfiguration // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *autoscalingv2.DescribeLaunchConfigurationsOutput, _ bool) bool {
		for _, lc := range page.LaunchConfigurations {
			l := &launchConfiguration{
				arn:  *lc.LaunchConfigurationARN,
				name: *lc.LaunchConfigurationName,
			}
			// No tags?
			if set.Mark(opts, l, lc.CreatedTime, nil) {
				logger.Warningf("%s: deleting %T: %s", l.ARN(), lc, l.name)
				if !opts.DryRun {
					toDelete = append(toDelete, l)
				}
			}
		}
		return true
	}

	if err := DescribeLaunchConfigurationsPages(svc, &autoscalingv2.DescribeLaunchConfigurationsInput{}, pageFunc); err != nil {
		return err
	}

	for _, lc := range toDelete {
		deleteReq := &autoscalingv2.DeleteLaunchConfigurationInput{
			LaunchConfigurationName: aws2.String(lc.name),
		}

		if _, err := svc.DeleteLaunchConfiguration(context.TODO(), deleteReq); err != nil {
			logger.Warningf("%s: delete failed: %v", lc.ARN(), err)
		}
	}

	return nil
}

func DescribeLaunchConfigurationsPages(svc *autoscalingv2.Client, input *autoscalingv2.DescribeLaunchConfigurationsInput, pageFunc func(page *autoscalingv2.DescribeLaunchConfigurationsOutput, _ bool) bool) error {
	paginator := autoscalingv2.NewDescribeLaunchConfigurationsPaginator(svc, input)

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

func (LaunchConfigurations) ListAll(opts Options) (*Set, error) {
	svc := autoscalingv2.NewFromConfig(*opts.Config, func(opt *autoscalingv2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	input := &autoscalingv2.DescribeLaunchConfigurationsInput{}

	err := DescribeLaunchConfigurationsPages(svc, input, func(lcs *autoscalingv2.DescribeLaunchConfigurationsOutput, isLast bool) bool {
		now := time.Now()
		for _, lc := range lcs.LaunchConfigurations {
			arn := launchConfiguration{
				arn:  *lc.LaunchConfigurationARN,
				name: *lc.LaunchConfigurationName,
			}.ARN()
			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't list launch configurations for %q in %q", opts.Account, opts.Region)
}

type launchConfiguration struct {
	arn  string
	name string
}

func (lc launchConfiguration) ARN() string {
	return lc.arn
}

func (lc launchConfiguration) ResourceKey() string {
	return lc.ARN()
}
