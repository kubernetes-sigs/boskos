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
	"context"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type S3Bucket struct{}

// IsManagedS3Bucket checks whether the bucket should be managed by janitor.
func IsManagedS3Bucket(opts Options, region string, bucketName string) (bool, error) {
	svc := s3.New(opts.Session, &aws.Config{Region: aws.String(region)})
	respTags, err := svc.GetBucketTagging(&s3.GetBucketTaggingInput{Bucket: aws.String(bucketName)})
	if err != nil {
		return false, err
	}
	tags := fromS3Tags(respTags.TagSet)
	return opts.ManagedPerTags(tags), nil
}

func (S3Bucket) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	if !opts.EnableS3BucketsClean {
		logger.Info("S3 bucket cleanup is not enabled")
		return nil
	}
	svc := s3.New(opts.Session, &aws.Config{Region: aws.String(opts.Region)})

	// ListBucket lists all buckets owned by the authenticated sender of the request
	// https://pkg.go.dev/github.com/aws/aws-sdk-go/service/s3#S3.ListBuckets
	// regardless the region configured in client.
	resp, err := svc.ListBuckets(&s3.ListBucketsInput{})
	if err != nil {
		return err
	}

	for _, b := range resp.Buckets {
		bucketName := *b.Name

		region, err := s3manager.GetBucketRegion(context.Background(), opts.Session, bucketName, opts.Region)
		if err != nil || region != opts.Region {
			continue
		}
		respTags, err := svc.GetBucketTagging(&s3.GetBucketTaggingInput{Bucket: aws.String(bucketName)})
		if err != nil {
			continue
		}
		tags := fromS3Tags(respTags.TagSet)
		if !set.Mark(opts, s3Bucket{bucketName: bucketName}, nil, tags) {
			continue
		}
		var objectsToDelete []*string
		pageFunc := func(page *s3.ListObjectsV2Output, _ bool) bool {
			for _, object := range page.Contents {
				logger.Warningf("Deleting object: %s at bucket: %s", *object.Key, bucketName)
				objectsToDelete = append(objectsToDelete, object.Key)
			}
			return true
		}
		// Before deleting the bucket, delete its objects.
		if err := svc.ListObjectsV2Pages(&s3.ListObjectsV2Input{Bucket: aws.String(bucketName)}, pageFunc); err != nil {
			logger.Warningf("ListObjectsV2Pages failed for bucket: %s : %v", bucketName, err)
			continue
		}

		logger.Warningf("Deleting bucket: %s", bucketName)
		if opts.DryRun {
			continue
		}
		if len(objectsToDelete) > 0 {
			for _, objectKey := range objectsToDelete {
				if _, err := svc.DeleteObject(&s3.DeleteObjectInput{Bucket: &bucketName, Key: objectKey}); err != nil {
					logger.Warningf("DeleteObject failed for bucket: %s, object: %s: %v", bucketName, *objectKey, err)
				}
			}
		}
		if _, err := svc.DeleteBucket(&s3.DeleteBucketInput{Bucket: &bucketName}); err != nil {
			logger.Warningf("DeleteBucket failed for bucket: %s : %v", bucketName, err)
		}
	}
	return nil
}

func (S3Bucket) ListAll(opts Options) (*Set, error) {
	set := NewSet(0)
	if !opts.EnableS3BucketsClean {
		return set, nil
	}
	svc := s3.New(opts.Session, &aws.Config{Region: aws.String(opts.Region)})

	// ListBucket lists all buckets owned by the authenticated sender of the request
	// https://pkg.go.dev/github.com/aws/aws-sdk-go/service/s3#S3.ListBuckets
	// regardless the region configured in client.
	resp, err := svc.ListBuckets(&s3.ListBucketsInput{})
	if err != nil {
		return set, err
	}
	for _, b := range resp.Buckets {
		bucketName := *b.Name
		region, err := s3manager.GetBucketRegion(context.Background(), opts.Session, bucketName, opts.Region)
		if err != nil || region != opts.Region {
			continue
		}
		now := time.Now()
		set.firstSeen[bucketName] = now
	}

	return set, errors.Wrapf(err, "couldn't list buckets for %q in %q", opts.Account, opts.Region)
}

type s3Bucket struct {
	bucketName string
}

func (s s3Bucket) ARN() string {
	return s.bucketName
}

func (s s3Bucket) ResourceKey() string {
	return s.ARN()
}
