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
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// LaunchConfigurations: http://docs.aws.amazon.com/sdk-for-go/api/service/autoscaling/#AutoScaling.DescribeLaunchConfigurations
type LaunchConfigurations struct{}

func (LaunchConfigurations) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := autoscaling.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	var toDelete []*launchConfiguration // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *autoscaling.DescribeLaunchConfigurationsOutput, _ bool) bool {
		for _, lc := range page.LaunchConfigurations {
			l := &launchConfiguration{
				arn:  aws.StringValue(lc.LaunchConfigurationARN),
				name: aws.StringValue(lc.LaunchConfigurationName),
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

	if err := svc.DescribeLaunchConfigurationsPages(&autoscaling.DescribeLaunchConfigurationsInput{}, pageFunc); err != nil {
		return err
	}

	for _, lc := range toDelete {
		deleteReq := &autoscaling.DeleteLaunchConfigurationInput{
			LaunchConfigurationName: aws.String(lc.name),
		}

		if _, err := svc.DeleteLaunchConfiguration(deleteReq); err != nil {
			logger.Warningf("%s: delete failed: %v", lc.ARN(), err)
		}
	}

	return nil
}

func (LaunchConfigurations) ListAll(opts Options) (*Set, error) {
	c := autoscaling.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(0)
	input := &autoscaling.DescribeLaunchConfigurationsInput{}

	err := c.DescribeLaunchConfigurationsPages(input, func(lcs *autoscaling.DescribeLaunchConfigurationsOutput, isLast bool) bool {
		now := time.Now()
		for _, lc := range lcs.LaunchConfigurations {
			arn := launchConfiguration{
				arn:  aws.StringValue(lc.LaunchConfigurationARN),
				name: aws.StringValue(lc.LaunchConfigurationName),
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
