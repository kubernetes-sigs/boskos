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
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Snapshots: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeSnapshots

type Snapshots struct{}

func (Snapshots) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	var toDelete []*snapshot // Paged call, defer deletion until we have the whole list.

	describeInput := &ec2.DescribeSnapshotsInput{
		// Exclude publicly-available snapshots from other owners.
		OwnerIds: aws.StringSlice([]string{"self"}),
	}

	pageFunc := func(page *ec2.DescribeSnapshotsOutput, _ bool) bool {
		for _, ss := range page.Snapshots {
			s := &snapshot{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      aws.StringValue(ss.SnapshotId),
			}
			// StartTime is probably close enough to a creation timestamp
			if set.Mark(s, ss.StartTime) {
				logger.Warningf("%s: deleting %T", s.ARN(), s)
				if !opts.DryRun {
					toDelete = append(toDelete, s)
				}
			}
		}
		return true
	}

	if err := svc.DescribeSnapshotsPages(describeInput, pageFunc); err != nil {
		return err
	}

	for _, ss := range toDelete {
		deleteInput := &ec2.DeleteSnapshotInput{
			SnapshotId: aws.String(ss.ID),
		}

		if _, err := svc.DeleteSnapshot(deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", ss.ARN(), err)
		}
	}

	return nil
}

func (Snapshots) ListAll(opts Options) (*Set, error) {
	c := ec2.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(0)
	input := &ec2.DescribeSnapshotsInput{
		// Exclude publicly-available snapshots from other owners.
		OwnerIds: aws.StringSlice([]string{"self"}),
	}

	err := c.DescribeSnapshotsPages(input, func(page *ec2.DescribeSnapshotsOutput, isLast bool) bool {
		now := time.Now()
		for _, ss := range page.Snapshots {
			arn := snapshot{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      aws.StringValue(ss.SnapshotId),
			}.ARN()
			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe snapshots for %q in %q", opts.Account, opts.Region)
}

type snapshot struct {
	Account string
	Region  string
	ID      string
}

func (s snapshot) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:snapshots/%s", s.Region, s.Account, s.ID)
}

func (s snapshot) ResourceKey() string {
	return s.ARN()
}
