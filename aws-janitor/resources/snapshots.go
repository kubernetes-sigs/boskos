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

	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Snapshots: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeSnapshots

type Snapshots struct{}

func (Snapshots) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	var toDelete []*snapshot // Paged call, defer deletion until we have the whole list.

	describeInput := &ec2v2.DescribeSnapshotsInput{
		// Exclude publicly-available snapshots from other owners.
		OwnerIds: []string{"self"},
	}

	pageFunc := func(page *ec2v2.DescribeSnapshotsOutput, _ bool) bool {
		for _, ss := range page.Snapshots {
			s := &snapshot{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      *ss.SnapshotId,
			}
			tags := fromEC2Tags(ss.Tags)
			// StartTime is probably close enough to a creation timestamp
			if !set.Mark(opts, s, ss.StartTime, tags) {
				continue
			}
			logger.Warningf("%s: deleting %T (%s)", s.ARN(), s, tags[NameTagKey])
			if !opts.DryRun {
				toDelete = append(toDelete, s)
			}
		}
		return true
	}

	if err := DescribeSnapshotsPages(svc, describeInput, pageFunc); err != nil {
		return err
	}

	for _, ss := range toDelete {
		deleteInput := &ec2v2.DeleteSnapshotInput{
			SnapshotId: &ss.ID,
		}

		if _, err := svc.DeleteSnapshot(context.TODO(), deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", ss.ARN(), err)
		}
	}

	return nil
}

func DescribeSnapshotsPages(svc *ec2v2.Client, input *ec2v2.DescribeSnapshotsInput, pageFunc func(page *ec2v2.DescribeSnapshotsOutput, _ bool) bool) error {
	paginator := ec2v2.NewDescribeSnapshotsPaginator(svc, input)

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

func (Snapshots) ListAll(opts Options) (*Set, error) {
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	input := &ec2v2.DescribeSnapshotsInput{
		// Exclude publicly-available snapshots from other owners.
		OwnerIds: []string{"self"},
	}

	err := DescribeSnapshotsPages(svc, input, func(page *ec2v2.DescribeSnapshotsOutput, isLast bool) bool {
		now := time.Now()
		for _, ss := range page.Snapshots {
			arn := snapshot{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      *ss.SnapshotId,
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
