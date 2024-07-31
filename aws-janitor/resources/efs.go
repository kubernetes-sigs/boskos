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
	"time"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	efsv2 "github.com/aws/aws-sdk-go-v2/service/efs"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	maxRetries   = 120
	pollInterval = time.Second
)

// ElasticFileSystems: https://docs.aws.amazon.com/sdk-for-go/api/service/efs/#EFS.DescribeFileSystems

type ElasticFileSystems struct{}

func (ElasticFileSystems) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := efsv2.NewFromConfig(*opts.Config, func(opt *efsv2.Options) {
		opt.Region = opts.Region
	})

	// Paged calls, defer deletion until we have the whole list.
	var (
		fileSystemsToDelete  []*elasticFileSystem
		mountTargetsToDelete []*mountTarget
	)

	// Mark and sweep file systems for deletion.
	fsPageFunc := func(page *efsv2.DescribeFileSystemsOutput, _ bool) bool {
		for _, fs := range page.FileSystems {
			f := &elasticFileSystem{
				id:  *fs.FileSystemId,
				arn: *fs.FileSystemArn,
			}
			tags := make(Tags, len(fs.Tags))
			for _, t := range fs.Tags {
				tags.Add(t.Key, t.Value)
			}
			if !set.Mark(opts, f, fs.CreationTime, tags) {
				continue
			}

			logger.Warningf("%s: deleting %T: %s (%s)", f.ARN(), fs, *fs.FileSystemId, *fs.Name)
			if !opts.DryRun {
				fileSystemsToDelete = append(fileSystemsToDelete, f)
			}
		}
		return true
	}
	if err := DescribeFileSystemsPages(svc, &efsv2.DescribeFileSystemsInput{}, fsPageFunc); err != nil {
		return err
	}

	// Collect mount targets for deletion.
	// These must be deleted before associated file systems can be deleted.
	mtPageFunc := func(page *efsv2.DescribeMountTargetsOutput, _ bool) bool {
		for _, mt := range page.MountTargets {
			m := &mountTarget{
				ID: *mt.MountTargetId,
			}
			logger.Warningf("%s: deleting %T", m.ID, mt)
			mountTargetsToDelete = append(mountTargetsToDelete, m)
		}
		return true
	}
	for _, fs := range fileSystemsToDelete {
		describeInput := &efsv2.DescribeMountTargetsInput{
			FileSystemId: aws2.String(fs.id),
		}
		if err := DescribeMountTargetsPages(svc, describeInput, mtPageFunc); err != nil {
			return err
		}
	}

	// Delete marked mount targets so we can delete the filesystems.
	if err := deleteMountTargetsAndWait(svc, fileSystemsToDelete, mountTargetsToDelete, logger); err != nil {
		return err
	}

	// Delete marked file systems.
	for _, fs := range fileSystemsToDelete {
		deleteInput := &efsv2.DeleteFileSystemInput{
			FileSystemId: aws2.String(fs.id),
		}
		if _, err := svc.DeleteFileSystem(context.TODO(), deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", fs.id, err)
		}
	}

	return nil
}

func DescribeMountTargetsPages(svc *efsv2.Client, input *efsv2.DescribeMountTargetsInput, pageFunc func(page *efsv2.DescribeMountTargetsOutput, _ bool) bool) error {
	paginator := efsv2.NewDescribeMountTargetsPaginator(svc, input)
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

func (ElasticFileSystems) ListAll(opts Options) (*Set, error) {
	svc := efsv2.NewFromConfig(*opts.Config, func(opt *efsv2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	input := &efsv2.DescribeFileSystemsInput{}

	err := DescribeFileSystemsPages(svc, input, func(page *efsv2.DescribeFileSystemsOutput, _ bool) bool {
		now := time.Now()
		for _, fs := range page.FileSystems {
			efs := elasticFileSystem{
				arn: *fs.FileSystemArn,
				id:  *fs.FileSystemId,
			}.ARN()
			set.firstSeen[efs] = now
		}
		return true
	})

	return set, errors.Wrapf(err, "couldn't describe auto scaling groups for %q in %q", opts.Account, opts.Region)
}

func DescribeFileSystemsPages(svc *efsv2.Client, input *efsv2.DescribeFileSystemsInput, pageFunc func(page *efsv2.DescribeFileSystemsOutput, _ bool) bool) error {
	paginator := efsv2.NewDescribeFileSystemsPaginator(svc, input)
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

type elasticFileSystem struct {
	arn string
	id  string
}

func (efs elasticFileSystem) ARN() string {
	return efs.arn
}

func (efs elasticFileSystem) ResourceKey() string {
	return efs.ARN()
}

type mountTarget struct {
	ID string
}

func deleteMountTargetsAndWait(svc *efsv2.Client, fileSystemsToDelete []*elasticFileSystem, mountTargetsToDelete []*mountTarget, logger *logrus.Entry) error {
	for _, mt := range mountTargetsToDelete {
		deleteInput := &efsv2.DeleteMountTargetInput{
			MountTargetId: aws2.String(mt.ID),
		}
		if _, err := svc.DeleteMountTarget(context.TODO(), deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", mt.ID, err)
		}
	}

	logger.Debug("waiting for mount targets to be deleted")
	for _, fs := range fileSystemsToDelete {
		describeInput := &efsv2.DescribeFileSystemsInput{
			FileSystemId: aws2.String(fs.id),
		}
		i := 0
		for ; i < maxRetries; i++ {
			describeOutput, err := svc.DescribeFileSystems(context.TODO(), describeInput)
			if err != nil {
				return err
			}
			if len(describeOutput.FileSystems) == 0 {
				logger.Warningf("%s: no filesystem found", fs.id)
				break
			}
			if describeOutput.FileSystems[0].NumberOfMountTargets == 0 {
				break
			}
			time.Sleep(pollInterval)
		}
		if i == maxRetries {
			logger.Warningf("%s: exceeded max retries polling file system status", fs.id)
		}
	}

	return nil
}
