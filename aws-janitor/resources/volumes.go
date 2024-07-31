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
	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Volumes: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeVolumes
type Volumes struct{}

func (Volumes) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	var toDelete []*volume // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *ec2v2.DescribeVolumesOutput, _ bool) bool {
		for _, vol := range page.Volumes {
			v := &volume{Account: opts.Account, Region: opts.Region, ID: *vol.VolumeId}
			tags := fromEC2Tags(vol.Tags)
			if !set.Mark(opts, v, vol.CreateTime, tags) {
				continue
			}
			// Since tags and other metadata may not propagate to volumes from their attachments,
			// we avoid deleting any volume that is currently attached. Once their attached resource is deleted,
			// we can safely delete volumes in a later clean-up run (using the mark data we saved in this run).
			if len(vol.Attachments) > 0 {
				continue
			}
			logger.Warningf("%s: deleting %T: %s (%s)", v.ARN(), vol, v.ID, tags[NameTagKey])
			if !opts.DryRun {
				toDelete = append(toDelete, v)
			}
		}
		return true
	}

	paginator := ec2v2.NewDescribeVolumesPaginator(svc, &ec2v2.DescribeVolumesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			logrus.Warningf("failed to get page, %v", err)
		} else {
			pageFunc(page, false)
		}
	}

	for _, vol := range toDelete {
		deleteReq := &ec2v2.DeleteVolumeInput{
			VolumeId: aws2.String(vol.ID),
		}

		if _, err := svc.DeleteVolume(context.TODO(), deleteReq); err != nil {
			logger.Warningf("%s: delete failed: %v", vol.ARN(), err)
		}
	}

	return nil
}

func (Volumes) ListAll(opts Options) (*Set, error) {
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	inp := &ec2v2.DescribeVolumesInput{}

	err := DescribeVolumesPages(svc, inp, func(vols *ec2v2.DescribeVolumesOutput, _ bool) bool {
		now := time.Now()
		for _, vol := range vols.Volumes {
			arn := volume{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      *vol.VolumeId,
			}.ARN()

			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe volumes for %q in %q", opts.Account, opts.Region)
}

func DescribeVolumesPages(svc *ec2v2.Client, input *ec2v2.DescribeVolumesInput, pageFunc func(vols *ec2v2.DescribeVolumesOutput, _ bool) bool) error {
	paginator := ec2v2.NewDescribeVolumesPaginator(svc, input)

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

type volume struct {
	Account string
	Region  string
	ID      string
}

func (vol volume) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:volume/%s", vol.Region, vol.Account, vol.ID)
}

func (vol volume) ResourceKey() string {
	return vol.ARN()
}
