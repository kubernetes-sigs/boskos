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

package s3

import (
	"context"
	"fmt"
	"net/url"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	s3v2 "github.com/aws/aws-sdk-go-v2/service/s3"
	"sigs.k8s.io/boskos/aws-janitor/regions"
)

type Path struct {
	Region string
	Bucket string
	Key    string
}

func GetPath(cfg *aws2.Config, s string) (*Path, error) {
	url, err := url.Parse(s)
	if err != nil {
		return nil, err
	}

	if url.Scheme != "s3" {
		return nil, fmt.Errorf("Scheme %q != 's3'", url.Scheme)
	}

	svc := s3v2.NewFromConfig(*cfg, func(options *s3v2.Options) {
		options.Region = regions.Default
	})

	resp, err := svc.GetBucketLocation(context.TODO(), &s3v2.GetBucketLocationInput{Bucket: aws2.String(url.Host)})
	if err != nil {
		return nil, err
	}

	region := resp.LocationConstraint

	return &Path{Region: string(region), Bucket: url.Host, Key: url.Path}, nil
}
