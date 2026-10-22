/*
Copyright 2026 The Kubernetes Authors.

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
	stderrors "errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3files"
	s3filestypes "github.com/aws/aws-sdk-go-v2/service/s3files/types"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type s3FilesClient interface {
	ListFileSystems(ctx context.Context, params *s3files.ListFileSystemsInput, optFns ...func(*s3files.Options)) (*s3files.ListFileSystemsOutput, error)
	ListTagsForResource(ctx context.Context, params *s3files.ListTagsForResourceInput, optFns ...func(*s3files.Options)) (*s3files.ListTagsForResourceOutput, error)
	ListMountTargets(ctx context.Context, params *s3files.ListMountTargetsInput, optFns ...func(*s3files.Options)) (*s3files.ListMountTargetsOutput, error)
	DeleteMountTarget(ctx context.Context, params *s3files.DeleteMountTargetInput, optFns ...func(*s3files.Options)) (*s3files.DeleteMountTargetOutput, error)
	ListAccessPoints(ctx context.Context, params *s3files.ListAccessPointsInput, optFns ...func(*s3files.Options)) (*s3files.ListAccessPointsOutput, error)
	DeleteAccessPoint(ctx context.Context, params *s3files.DeleteAccessPointInput, optFns ...func(*s3files.Options)) (*s3files.DeleteAccessPointOutput, error)
	DeleteFileSystem(ctx context.Context, params *s3files.DeleteFileSystemInput, optFns ...func(*s3files.Options)) (*s3files.DeleteFileSystemOutput, error)
}

// s3FilesMountTargetWait bounds how long a sweep waits for the mount targets
// of one file system to be deleted before giving up on that file system until
// the next run. The values match the EFS mount target wait.
type s3FilesMountTargetWait struct {
	attempts int
	interval time.Duration
}

var defaultS3FilesMountTargetWait = s3FilesMountTargetWait{
	attempts: maxRetries,
	interval: pollInterval,
}

// S3FilesFileSystems manages Amazon S3 Files file systems and their mount
// targets. Mount targets own network interfaces, so this type must run before
// NetworkInterfaces, Subnets and SecurityGroups.
type S3FilesFileSystems struct{}

func (S3FilesFileSystems) MarkAndSweep(opts Options, set *Set) error {
	if !opts.EnableS3FilesClean {
		logrus.WithField("options", opts).Info("S3 Files cleanup is not enabled")
		return nil
	}
	svc := s3files.NewFromConfig(*opts.Config, func(opt *s3files.Options) {
		opt.Region = opts.Region
	})
	return markAndSweepS3FilesFileSystems(svc, opts, set, defaultS3FilesMountTargetWait)
}

func markAndSweepS3FilesFileSystems(svc s3FilesClient, opts Options, set *Set, wait s3FilesMountTargetWait) error {
	logger := logrus.WithField("options", opts)
	fileSystems, err := listS3FilesFileSystems(svc)
	if err != nil {
		if skip, reason := skippableS3FilesListError(err); skip {
			logger.Warningf("Skipping S3 Files file systems: %s: %v", reason, err)
			return nil
		}
		return errors.Wrapf(err, "couldn't list S3 Files file systems for %q in %q", opts.Account, opts.Region)
	}

	// Finish discovery before issuing deletions so a pagination failure cannot
	// leave the account only partially swept.
	toDelete := make([]*s3FilesFileSystem, 0, len(fileSystems))
	for _, description := range fileSystems {
		fileSystem, err := newS3FilesFileSystem(description)
		if err != nil {
			logger.Warningf("couldn't read an S3 Files file system from the list response: %v", err)
			return err
		}

		// Tags are not part of the list response. A file system whose tags
		// cannot be read is left alone, because its exclude tags are unknown.
		tags, err := listS3FilesTags(svc, fileSystem.id)
		if err != nil {
			if isS3FilesNotFound(err) {
				logger.Infof("%s: skipping, no tags found", fileSystem.ARN())
			} else {
				logger.Warningf("%s: skipping, couldn't list tags: %v", fileSystem.ARN(), err)
			}
			continue
		}
		if !set.Mark(opts, fileSystem, description.CreationTime, tags) {
			logger.Debugf("%s: skipping, not managed by this janitor or not yet old enough", fileSystem.ARN())
			continue
		}
		if description.Status == s3filestypes.LifeCycleStateDeleting || description.Status == s3filestypes.LifeCycleStateDeleted {
			logger.Infof("%s: already %s", fileSystem.ARN(), description.Status)
			continue
		}

		logger.Infof("%s: deleting %T: %s", fileSystem.ARN(), description, fileSystem.id)
		if !opts.DryRun {
			toDelete = append(toDelete, fileSystem)
		}
	}

	for _, fileSystem := range toDelete {
		// Access points are children of the file system and are not removed by
		// deleting it, so they go first.
		deleteS3FilesAccessPoints(svc, fileSystem, logger)

		deleted, err := deleteS3FilesMountTargetsAndWait(svc, fileSystem, wait, logger)
		if err != nil {
			logger.Warningf("%s: couldn't delete mount targets: %v", fileSystem.ARN(), err)
			continue
		}
		if !deleted {
			logger.Warningf("%s: mount targets are still being deleted, will retry on the next run", fileSystem.ARN())
			continue
		}

		// ForceDelete discards data that has not yet been exported to the
		// backing S3 bucket. Without it, a file system with pending exports
		// could never be cleaned up.
		input := &s3files.DeleteFileSystemInput{
			FileSystemId: aws.String(fileSystem.id),
			ForceDelete:  aws.Bool(true),
		}
		if _, err := svc.DeleteFileSystem(context.TODO(), input); err != nil && !isS3FilesNotFound(err) {
			logger.Warningf("%s: delete failed: %v", fileSystem.ARN(), err)
		}
	}

	return nil
}

// deleteS3FilesAccessPoints deletes every access point of the file system.
// Access points are deleted asynchronously but do not hold a network
// interface, so the sweep does not wait for them.
func deleteS3FilesAccessPoints(svc s3FilesClient, fileSystem *s3FilesFileSystem, logger *logrus.Entry) {
	accessPoints, err := listS3FilesAccessPoints(svc, fileSystem.id)
	if err != nil {
		logger.Warningf("%s: couldn't list access points: %v", fileSystem.ARN(), err)
		return
	}

	for _, accessPoint := range accessPoints {
		if accessPoint.Status == s3filestypes.LifeCycleStateDeleting || accessPoint.Status == s3filestypes.LifeCycleStateDeleted {
			continue
		}
		id := aws.ToString(accessPoint.AccessPointId)
		logger.Infof("%s: deleting access point %s", fileSystem.ARN(), id)
		input := &s3files.DeleteAccessPointInput{AccessPointId: accessPoint.AccessPointId}
		if _, err := svc.DeleteAccessPoint(context.TODO(), input); err != nil && !isS3FilesNotFound(err) {
			logger.Warningf("%s: delete failed: %v", id, err)
		}
	}
}

// deleteS3FilesMountTargetsAndWait deletes every mount target of the file
// system and reports whether they are all gone within the wait bound.
func deleteS3FilesMountTargetsAndWait(svc s3FilesClient, fileSystem *s3FilesFileSystem, wait s3FilesMountTargetWait, logger *logrus.Entry) (bool, error) {
	mountTargets, err := liveS3FilesMountTargets(svc, fileSystem.id)
	if err != nil {
		return false, err
	}
	if len(mountTargets) == 0 {
		return true, nil
	}

	for _, mountTarget := range mountTargets {
		if mountTarget.Status == s3filestypes.LifeCycleStateDeleting {
			continue
		}
		id := aws.ToString(mountTarget.MountTargetId)
		logger.Infof("%s: deleting mount target %s", fileSystem.ARN(), id)
		input := &s3files.DeleteMountTargetInput{MountTargetId: mountTarget.MountTargetId}
		if _, err := svc.DeleteMountTarget(context.TODO(), input); err != nil && !isS3FilesNotFound(err) {
			logger.Warningf("%s: delete failed: %v", id, err)
		}
	}

	for i := 0; i < wait.attempts; i++ {
		time.Sleep(wait.interval)
		remaining, err := liveS3FilesMountTargets(svc, fileSystem.id)
		if err != nil {
			return false, err
		}
		if len(remaining) == 0 {
			return true, nil
		}
	}
	return false, nil
}

func (S3FilesFileSystems) ListAll(opts Options) (*Set, error) {
	if !opts.EnableS3FilesClean {
		return NewSet(0), nil
	}
	svc := s3files.NewFromConfig(*opts.Config, func(opt *s3files.Options) {
		opt.Region = opts.Region
	})
	return listAllS3FilesFileSystems(svc, opts)
}

func listAllS3FilesFileSystems(svc s3FilesClient, opts Options) (*Set, error) {
	set := NewSet(0)
	fileSystems, err := listS3FilesFileSystems(svc)
	if err != nil {
		if skip, reason := skippableS3FilesListError(err); skip {
			logrus.WithField("options", opts).Warningf("Skipping S3 Files file systems: %s: %v", reason, err)
			return set, nil
		}
		return set, errors.Wrapf(err, "couldn't list S3 Files file systems for %q in %q", opts.Account, opts.Region)
	}

	now := time.Now()
	for _, description := range fileSystems {
		fileSystem, err := newS3FilesFileSystem(description)
		if err != nil {
			logrus.WithField("options", opts).Warningf("couldn't read an S3 Files file system from the list response: %v", err)
			return set, err
		}
		set.firstSeen[fileSystem.ResourceKey()] = now
	}

	return set, nil
}

func listS3FilesFileSystems(svc s3FilesClient) ([]s3filestypes.ListFileSystemsDescription, error) {
	paginator := s3files.NewListFileSystemsPaginator(svc, &s3files.ListFileSystemsInput{})
	var fileSystems []s3filestypes.ListFileSystemsDescription

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			return nil, err
		}
		fileSystems = append(fileSystems, page.FileSystems...)
	}

	return fileSystems, nil
}

// liveS3FilesMountTargets lists the mount targets of the file system that
// still exist. A mount target reported as deleted no longer holds a network
// interface, so it must not keep the sweep waiting.
func liveS3FilesMountTargets(svc s3FilesClient, fileSystemID string) ([]s3filestypes.ListMountTargetsDescription, error) {
	paginator := s3files.NewListMountTargetsPaginator(svc, &s3files.ListMountTargetsInput{
		FileSystemId: aws.String(fileSystemID),
	})
	var mountTargets []s3filestypes.ListMountTargetsDescription

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			if isS3FilesNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, mountTarget := range page.MountTargets {
			if mountTarget.Status == s3filestypes.LifeCycleStateDeleted {
				continue
			}
			mountTargets = append(mountTargets, mountTarget)
		}
	}

	return mountTargets, nil
}

func listS3FilesAccessPoints(svc s3FilesClient, fileSystemID string) ([]s3filestypes.ListAccessPointsDescription, error) {
	paginator := s3files.NewListAccessPointsPaginator(svc, &s3files.ListAccessPointsInput{
		FileSystemId: aws.String(fileSystemID),
	})
	var accessPoints []s3filestypes.ListAccessPointsDescription

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			if isS3FilesNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
		accessPoints = append(accessPoints, page.AccessPoints...)
	}

	return accessPoints, nil
}

func listS3FilesTags(svc s3FilesClient, fileSystemID string) (Tags, error) {
	paginator := s3files.NewListTagsForResourcePaginator(svc, &s3files.ListTagsForResourceInput{
		ResourceId: aws.String(fileSystemID),
	})
	tags := Tags{}

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			return nil, err
		}
		for i, tag := range page.Tags {
			if tag.Key == nil || *tag.Key == "" {
				return nil, fmt.Errorf("tag %d has no key", i)
			}
			tags[*tag.Key] = aws.ToString(tag.Value)
		}
	}

	return tags, nil
}

// skippableS3FilesListError reports whether a failure to list file systems
// means there is nothing this janitor can clean in the region, rather than a
// real error. S3 Files is not available in every region, and the janitor's
// credentials may not grant access to it. Returning an error in those cases
// would fail every run of a janitor that sweeps all regions. The caller logs
// the reason together with the error so a missing permission is visible.
func skippableS3FilesListError(err error) (bool, string) {
	var dnsErr *net.DNSError
	if stderrors.As(err, &dnsErr) {
		return true, fmt.Sprintf("service endpoint %q is not reachable in this region", dnsErr.Name)
	}
	var responseErr *awshttp.ResponseError
	if stderrors.As(err, &responseErr) && responseErr.HTTPStatusCode() == http.StatusForbidden {
		return true, fmt.Sprintf("access denied (HTTP %d, request %q), the janitor may be missing s3files permissions", responseErr.HTTPStatusCode(), responseErr.ServiceRequestID())
	}
	return false, ""
}

func isS3FilesNotFound(err error) bool {
	var notFound *s3filestypes.ResourceNotFoundException
	return stderrors.As(err, &notFound)
}

func newS3FilesFileSystem(description s3filestypes.ListFileSystemsDescription) (*s3FilesFileSystem, error) {
	if description.FileSystemId == nil || *description.FileSystemId == "" {
		return nil, errors.New("service returned an S3 Files file system without an ID")
	}
	if description.FileSystemArn == nil || *description.FileSystemArn == "" {
		return nil, fmt.Errorf("S3 Files file system %q has no ARN", *description.FileSystemId)
	}

	return &s3FilesFileSystem{
		arn: *description.FileSystemArn,
		id:  *description.FileSystemId,
	}, nil
}

type s3FilesFileSystem struct {
	arn string
	id  string
}

func (fileSystem s3FilesFileSystem) ARN() string {
	return fileSystem.arn
}

func (fileSystem s3FilesFileSystem) ResourceKey() string {
	return fileSystem.ARN()
}
