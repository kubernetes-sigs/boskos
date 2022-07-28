/*
Copyright 2022 The Kubernetes Authors.

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

// Clean-up ELBV2 target groups.

type TargetGroups struct{}

func (TargetGroups) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	if !opts.EnableTargetGroupClean {
		logger.Info("Disable target group clean")
		return nil
	}
	svc := elbv2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	var targetGroups []*targetGroup
	tgTags := make(map[string]Tags)

	pageFunc := func(page *elbv2.DescribeTargetGroupsOutput, _ bool) bool {
		for _, tg := range page.TargetGroups {
			a := &targetGroup{
				arn: aws.StringValue(tg.TargetGroupArn),
			}
			targetGroups = append(targetGroups, a)
			tgTags[a.ARN()] = nil
		}
		return true
	}

	if err := svc.DescribeTargetGroupsPages(&elbv2.DescribeTargetGroupsInput{}, pageFunc); err != nil {
		return err
	}

	fetchTagErr := incrementalFetchTags(tgTags, 20, func(tgArns []*string) error {
		tagsResp, err := svc.DescribeTags(&elbv2.DescribeTagsInput{ResourceArns: tgArns})
		if err != nil {
			return err
		}

		var errs []error
		for _, tagDesc := range tagsResp.TagDescriptions {
			arn := aws.StringValue(tagDesc.ResourceArn)
			_, ok := tgTags[arn]
			if !ok {
				errs = append(errs, fmt.Errorf("unknown target group ARN in tag response: %s", arn))
				continue
			}
			if tgTags[arn] == nil {
				tgTags[arn] = make(Tags, len(tagDesc.Tags))
			}
			for _, t := range tagDesc.Tags {
				tgTags[arn].Add(t.Key, t.Value)
			}
		}
		return kerrors.NewAggregate(errs)
	})

	if fetchTagErr != nil {
		return fetchTagErr
	}

	for _, tg := range targetGroups {
		if !set.Mark(opts, tg, nil, tgTags[tg.ARN()]) {
			continue
		}
		logger.Warningf("%s: deleting %T", tg.ARN(), tg)

		if opts.DryRun {
			continue
		}

		deleteInput := &elbv2.DeleteTargetGroupInput{
			TargetGroupArn: aws.String(tg.ARN()),
		}

		if _, err := svc.DeleteTargetGroup(deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", tg.ARN(), err)
		}
	}

	return nil
}

func (TargetGroups) ListAll(opts Options) (*Set, error) {
	set := NewSet(0)
	if !opts.EnableTargetGroupClean {
		return set, nil
	}
	c := elbv2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	input := &elbv2.DescribeTargetGroupsInput{}

	err := c.DescribeTargetGroupsPages(input, func(tgs *elbv2.DescribeTargetGroupsOutput, isLast bool) bool {
		now := time.Now()
		for _, tg := range tgs.TargetGroups {
			a := &targetGroup{arn: *tg.TargetGroupArn}
			set.firstSeen[a.ARN()] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe target groups for %q in %q", opts.Account, opts.Region)
}

type targetGroup struct {
	arn string
}

func (tg targetGroup) ARN() string {
	return tg.arn
}

func (tg targetGroup) ResourceKey() string {
	return tg.ARN()
}
