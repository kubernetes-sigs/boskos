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
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/efs"
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
	svc := efs.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	// Paged calls, defer deletion until we have the whole list.
	var (
		fileSystemsToDelete  []*elasticFileSystem
		mountTargetsToDelete []*mountTarget
	)

	// Mark and sweep file systems for deletion.
	fsPageFunc := func(page *efs.DescribeFileSystemsOutput, _ bool) bool {
		for _, fs := range page.FileSystems {
			f := &elasticFileSystem{
				id:  aws.StringValue(fs.FileSystemId),
				arn: aws.StringValue(fs.FileSystemArn),
			}
			tags := make([]Tag, len(fs.Tags))
			for _, t := range fs.Tags {
				tags = append(tags, NewTag(t.Key, t.Value))
			}
			if !set.Mark(opts, f, fs.CreationTime, tags) {
				continue
			}

			logger.Warningf("%s: deleting %T: %s", f.ARN(), fs, *fs.Name)
			if !opts.DryRun {
				fileSystemsToDelete = append(fileSystemsToDelete, f)
			}
		}
		return true
	}
	if err := svc.DescribeFileSystemsPages(&efs.DescribeFileSystemsInput{}, fsPageFunc); err != nil {
		return err
	}

	// Collect mount targets for deletion.
	// These must be deleted before associated file systems can be deleted.
	mtPageFunc := func(page *efs.DescribeMountTargetsOutput, _ bool) bool {
		for _, mt := range page.MountTargets {
			m := &mountTarget{
				ID: aws.StringValue(mt.MountTargetId),
			}
			logger.Warningf("%s: deleting %T", m.ID, mt)
			mountTargetsToDelete = append(mountTargetsToDelete, m)
		}
		return true
	}
	for _, fs := range fileSystemsToDelete {
		describeInput := &efs.DescribeMountTargetsInput{
			FileSystemId: aws.String(fs.id),
		}
		if err := describeMountTargetsPages(svc, describeInput, mtPageFunc); err != nil {
			return err
		}
	}

	// Delete marked mount targets so we can delete the filesystems.
	if err := deleteMountTargetsAndWait(svc, fileSystemsToDelete, mountTargetsToDelete, logger); err != nil {
		return err
	}

	// Delete marked file systems.
	for _, fs := range fileSystemsToDelete {
		deleteInput := &efs.DeleteFileSystemInput{
			FileSystemId: aws.String(fs.id),
		}
		if _, err := svc.DeleteFileSystem(deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", fs.id, err)
		}
	}

	return nil
}

func (ElasticFileSystems) ListAll(opts Options) (*Set, error) {
	svc := efs.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(0)
	input := &efs.DescribeFileSystemsInput{}

	err := svc.DescribeFileSystemsPages(input, func(page *efs.DescribeFileSystemsOutput, _ bool) bool {
		now := time.Now()
		for _, fs := range page.FileSystems {
			efs := elasticFileSystem{
				arn: aws.StringValue(fs.FileSystemArn),
				id:  aws.StringValue(fs.FileSystemId),
			}.ARN()
			set.firstSeen[efs] = now
		}
		return true
	})

	return set, errors.Wrapf(err, "couldn't describe auto scaling groups for %q in %q", opts.Account, opts.Region)
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

func deleteMountTargetsAndWait(svc *efs.EFS, fileSystemsToDelete []*elasticFileSystem, mountTargetsToDelete []*mountTarget, logger *logrus.Entry) error {
	for _, mt := range mountTargetsToDelete {
		deleteInput := &efs.DeleteMountTargetInput{
			MountTargetId: aws.String(mt.ID),
		}
		if _, err := svc.DeleteMountTarget(deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", mt.ID, err)
		}
	}

	logger.Debug("waiting for mount targets to be deleted")
	for _, fs := range fileSystemsToDelete {
		describeInput := &efs.DescribeFileSystemsInput{
			FileSystemId: aws.String(fs.id),
		}
		i := 0
		for ; i < maxRetries; i++ {
			describeOutput, err := svc.DescribeFileSystems(describeInput)
			if err != nil {
				return err
			}
			if len(describeOutput.FileSystems) == 0 {
				logger.Warningf("%s: no filesystem found", fs.id)
				break
			}
			if *describeOutput.FileSystems[0].NumberOfMountTargets == 0 {
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

// Provides pagination-handling for reading EFS mount targets.
// Implementation borrowed from the AWS SDK.
func describeMountTargetsPages(
	client *efs.EFS,
	input *efs.DescribeMountTargetsInput,
	fn func(*efs.DescribeMountTargetsOutput, bool) bool,
) error {
	return describeMountTargetsPagesWithContext(client, aws.BackgroundContext(), input, fn)
}

func describeMountTargetsPagesWithContext(
	client *efs.EFS,
	ctx aws.Context,
	input *efs.DescribeMountTargetsInput,
	fn func(*efs.DescribeMountTargetsOutput, bool) bool,
	opts ...request.Option,
) error {
	p := request.Pagination{
		NewRequest: func() (*request.Request, error) {
			var inCpy *efs.DescribeMountTargetsInput
			if input != nil {
				tmp := *input
				inCpy = &tmp
			}
			req, _ := client.DescribeMountTargetsRequest(inCpy)
			req.SetContext(ctx)
			req.ApplyOptions(opts...)
			return req, nil
		},
	}

	for p.Next() {
		if !fn(p.Page().(*efs.DescribeMountTargetsOutput), !p.HasNextPage()) {
			break
		}
	}

	return p.Err()
}
