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

	cfv2 "github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfv2types "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

// Cloud Formation Stacks
type CloudFormationStacks struct{}

func (CloudFormationStacks) fetchTags(svc *cfv2.Client, stackID string) (Tags, error) {
	tags := make(Tags)
	var errs []error

	describeErr := DescribeStacksPages(svc, &cfv2.DescribeStacksInput{StackName: &stackID},
		func(page *cfv2.DescribeStacksOutput, _ bool) bool {
			for _, stack := range page.Stacks {
				if *stack.StackId != stackID {
					errs = append(errs, fmt.Errorf("unexpected stack id in DescribeStacks output: %s", *stack.StackId))
					continue
				}
				for _, t := range stack.Tags {
					tags.Add(t.Key, t.Value)
				}
			}
			return true
		})
	if describeErr != nil {
		errs = append(errs, describeErr)
	}
	return tags, kerrors.NewAggregate(errs)
}

func (cfs CloudFormationStacks) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := cfv2.NewFromConfig(*opts.Config, func(opt *cfv2.Options) {
		opt.Region = opts.Region
	})

	var toDelete []*cloudFormationStack // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *cfv2.ListStacksOutput, _ bool) bool {
		for _, stack := range page.StackSummaries {
			// Do not delete stacks that are already deleted or are being
			// deleted.
			switch stack.StackStatus {
			case cfv2types.StackStatusDeleteComplete,
				cfv2types.StackStatusDeleteInProgress:
				continue
			}
			o := &cloudFormationStack{
				arn:  *stack.StackId,
				name: *stack.StackName,
			}
			tags, tagErr := cfs.fetchTags(svc, o.arn)
			if tagErr != nil {
				logger.Warningf("%s: failed to fetch tags: %v", o.ARN(), tagErr)
				continue
			}
			if !set.Mark(opts, o, stack.CreationTime, tags) {
				continue
			}

			logger.Warningf("%s: deleting %T: %s", o.ARN(), o, o.name)
			if !opts.DryRun {
				toDelete = append(toDelete, o)
			}
		}
		return true
	}

	if err := ListStacksPages(svc, &cfv2.ListStacksInput{}, pageFunc); err != nil {
		return err
	}

	for _, o := range toDelete {
		if err := o.delete(svc); err != nil {
			logger.Warningf("%s: delete failed: %v", o.ARN(), err)
		}
	}
	return nil
}

func (CloudFormationStacks) ListAll(opts Options) (*Set, error) {
	svc := cfv2.NewFromConfig(*opts.Config, func(opt *cfv2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	inp := &cfv2.ListStacksInput{}

	err := ListStacksPages(svc, inp, func(stacks *cfv2.ListStacksOutput, _ bool) bool {
		now := time.Now()
		for _, stack := range stacks.StackSummaries {
			arn := cloudFormationStack{
				arn:  *stack.StackId,
				name: *stack.StackName,
			}.ARN()
			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe cloud formation stacks for %q in %q", opts.Account, opts.Region)
}

func ListStacksPages(svc *cfv2.Client, input *cfv2.ListStacksInput, pageFunc func(stacks *cfv2.ListStacksOutput, _ bool) bool) error {
	paginator := cfv2.NewListStacksPaginator(svc, input)

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

func DescribeStacksPages(svc *cfv2.Client, input *cfv2.DescribeStacksInput, pageFunc func(page *cfv2.DescribeStacksOutput, _ bool) bool) error {
	paginator := cfv2.NewDescribeStacksPaginator(svc, input)

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

type cloudFormationStack struct {
	arn  string
	name string
}

func (p cloudFormationStack) ARN() string {
	return p.arn
}

func (p cloudFormationStack) ResourceKey() string {
	return p.ARN()
}

func (p cloudFormationStack) delete(svc *cfv2.Client) error {
	input := &cfv2.DeleteStackInput{StackName: &p.name}
	if _, err := svc.DeleteStack(context.TODO(), input); err != nil {
		return err
	}
	return nil
}
