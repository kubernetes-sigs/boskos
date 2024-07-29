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

	s3v2manager "github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	s3v2 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type S3Bucket struct{}

// IsManagedS3Bucket checks whether the bucket should be managed by janitor.
func IsManagedS3Bucket(opts Options, region string, bucketName string) (bool, error) {
	svc := s3v2.NewFromConfig(*opts.Config, func(opt *s3v2.Options) {
		opt.Region = opts.Region
	})
	respTags, err := svc.GetBucketTagging(context.TODO(), &s3v2.GetBucketTaggingInput{Bucket: &bucketName})
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
	svc := s3v2.NewFromConfig(*opts.Config, func(opt *s3v2.Options) {
		opt.Region = opts.Region
	})

	// ListBucket lists all buckets owned by the authenticated sender of the request
	// https://pkg.go.dev/github.com/aws/aws-sdk-go/service/s3#S3.ListBuckets
	// regardless the region configured in client.
	resp, err := svc.ListBuckets(context.TODO(), &s3v2.ListBucketsInput{})
	if err != nil {
		return err
	}

	for _, b := range resp.Buckets {
		bucketName := *b.Name

		region, err := s3v2manager.GetBucketRegion(context.Background(), svc, bucketName)
		if err != nil || region != opts.Region {
			continue
		}
		respTags, err := svc.GetBucketTagging(context.TODO(), &s3v2.GetBucketTaggingInput{Bucket: &bucketName})
		if err != nil {
			continue
		}
		tags := fromS3Tags(respTags.TagSet)
		if !set.Mark(opts, s3Bucket{bucketName: bucketName}, nil, tags) {
			continue
		}
		var objectsToDelete []*string
		pageFunc := func(page *s3v2.ListObjectsV2Output, _ bool) bool {
			for _, object := range page.Contents {
				logger.Warningf("Deleting object: %s at bucket: %s", *object.Key, bucketName)
				objectsToDelete = append(objectsToDelete, object.Key)
			}
			return true
		}
		// Before deleting the bucket, delete its objects.
		if err := ListObjectsV2Pages(svc, &s3v2.ListObjectsV2Input{Bucket: &bucketName}, pageFunc); err != nil {
			logger.Warningf("ListObjectsV2Pages failed for bucket: %s : %v", bucketName, err)
			continue
		}

		logger.Warningf("Deleting bucket: %s", bucketName)
		if opts.DryRun {
			continue
		}
		if len(objectsToDelete) > 0 {
			for _, objectKey := range objectsToDelete {
				if _, err := svc.DeleteObject(context.TODO(), &s3v2.DeleteObjectInput{Bucket: &bucketName, Key: objectKey}); err != nil {
					logger.Warningf("DeleteObject failed for bucket: %s, object: %s: %v", bucketName, *objectKey, err)
				}
			}
		}
		if _, err := svc.DeleteBucket(context.TODO(), &s3v2.DeleteBucketInput{Bucket: &bucketName}); err != nil {
			logger.Warningf("DeleteBucket failed for bucket: %s : %v", bucketName, err)
		}
	}
	return nil
}

func ListObjectsV2Pages(svc *s3v2.Client, input *s3v2.ListObjectsV2Input, pageFunc func(page *s3v2.ListObjectsV2Output, _ bool) bool) error {
	paginator := s3v2.NewListObjectsV2Paginator(svc, input)
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

func (S3Bucket) ListAll(opts Options) (*Set, error) {
	set := NewSet(0)
	if !opts.EnableS3BucketsClean {
		return set, nil
	}

	svc := s3v2.NewFromConfig(*opts.Config, func(opt *s3v2.Options) {
		opt.Region = opts.Region
	})

	// ListBucket lists all buckets owned by the authenticated sender of the request
	// https://pkg.go.dev/github.com/aws/aws-sdk-go/service/s3#S3.ListBuckets
	// regardless the region configured in client.
	resp, err := svc.ListBuckets(context.TODO(), &s3v2.ListBucketsInput{})
	if err != nil {
		return set, err
	}
	for _, b := range resp.Buckets {
		bucketName := *b.Name
		region, err := s3v2manager.GetBucketRegion(context.Background(), svc, bucketName)
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
