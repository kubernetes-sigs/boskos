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
	cf "github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Cloud Formation Stacks
type CloudFormationStacks struct{}

func (CloudFormationStacks) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := cf.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	var toDelete []*cloudFormationStack // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *cf.ListStacksOutput, _ bool) bool {
		for _, stack := range page.StackSummaries {
			// Do not delete stacks that are already deleted or are being
			// deleted.
			switch aws.StringValue(stack.StackStatus) {
			case cf.ResourceStatusDeleteComplete,
				cf.ResourceStatusDeleteInProgress:
				continue
			}
			o := &cloudFormationStack{
				id:   aws.StringValue(stack.StackId),
				name: aws.StringValue(stack.StackName),
			}
			if set.Mark(o) {
				logger.Warningf("%s: deleting %T: %s", o.ARN(), o, o.name)
				if !opts.DryRun {
					toDelete = append(toDelete, o)
				}
			}
		}
		return true
	}

	if err := svc.ListStacksPages(&cf.ListStacksInput{}, pageFunc); err != nil {
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
	svc := cf.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(0)
	inp := &cf.ListStacksInput{}

	err := svc.ListStacksPages(inp, func(stacks *cf.ListStacksOutput, _ bool) bool {
		now := time.Now()
		for _, stack := range stacks.StackSummaries {
			set.firstSeen[aws.StringValue(stack.StackId)] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe cloud formation stacks for %q in %q", opts.Account, opts.Region)
}

type cloudFormationStack struct {
	id   string
	name string
}

func (p cloudFormationStack) ARN() string {
	return p.id
}

func (p cloudFormationStack) ResourceKey() string {
	return p.name
}

func (p cloudFormationStack) delete(svc *cf.CloudFormation) error {
	input := &cf.DeleteStackInput{StackName: aws.String(p.name)}
	if _, err := svc.DeleteStack(input); err != nil {
		return err
	}
	return nil
}
