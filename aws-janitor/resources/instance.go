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
	"strings"
	"time"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Instances: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeInstances

type Instances struct{}

func (Instances) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	inp := &ec2v2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws2.String("instance-state-name"),
				Values: []string{"running", "pending"},
			},
		},
	}

	var toDelete []string // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *ec2v2.DescribeInstancesOutput, _ bool) bool {
		for _, res := range page.Reservations {
			for _, inst := range res.Instances {
				i := &instance{
					Account:    opts.Account,
					Region:     opts.Region,
					InstanceID: *inst.InstanceId,
				}
				tags := fromEC2Tags(inst.Tags)
				// Instances don't have a creation date, but launch time is better than nothing.
				if !set.Mark(opts, i, inst.LaunchTime, tags) {
					continue
				}

				logger.Warningf("%s: deleting %T: %s (%s)", i.ARN(), inst, i.InstanceID, tags[NameTagKey])
				if !opts.DryRun {
					toDelete = append(toDelete, *inst.InstanceId)
				}
			}
		}
		return true
	}

	if err := DescribeInstancesPages(svc, inp, pageFunc); err != nil {
		return err
	}

	if len(toDelete) > 0 {
		// TODO(zmerlynn): In theory this should be split up into
		// blocks of 1000, but burn that bridge if it ever happens...
		if _, err := svc.TerminateInstances(context.TODO(), &ec2v2.TerminateInstancesInput{InstanceIds: toDelete}); err != nil {
			logger.Warningf("Termination failed for instances: %s : %v", strings.Join(toDelete, ", "), err)
		}
	}

	return nil
}

func DescribeInstancesPages(svc *ec2v2.Client, inp *ec2v2.DescribeInstancesInput, pageFunc func(page *ec2v2.DescribeInstancesOutput, _ bool) bool) error {
	paginator := ec2v2.NewDescribeInstancesPaginator(svc, inp)

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

func (Instances) ListAll(opts Options) (*Set, error) {
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	inp := &ec2v2.DescribeInstancesInput{}

	err := DescribeInstancesPages(svc, inp, func(instances *ec2v2.DescribeInstancesOutput, _ bool) bool {
		for _, res := range instances.Reservations {
			for _, inst := range res.Instances {
				now := time.Now()
				arn := instance{
					Account:    opts.Account,
					Region:     opts.Region,
					InstanceID: *inst.InstanceId,
				}.ARN()

				set.firstSeen[arn] = now
			}
		}
		return true

	})

	return set, errors.Wrapf(err, "couldn't describe instances for %q in %q", opts.Account, opts.Region)
}

type instance struct {
	Account    string
	Region     string
	InstanceID string
}

func (i instance) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:instance/%s", i.Region, i.Account, i.InstanceID)
}

func (i instance) ResourceKey() string {
	return i.ARN()
}
