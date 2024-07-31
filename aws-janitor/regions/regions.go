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

package regions

import (
	"context"
	"fmt"
	"os"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Default is the region we use when no region is applicable
var Default string

func init() {
	if Default = os.Getenv("AWS_DEFAULT_REGION"); Default == "" {
		Default = "us-east-1"
	}
}

// GetAll retrieves all regions from the AWS API
func GetAll(cfg *aws2.Config) ([]string, error) {
	var regions []string
	svc := ec2v2.NewFromConfig(*cfg, func(options *ec2v2.Options) {
		options.Region = Default
	})
	resp, err := svc.DescribeRegions(context.TODO(), nil)
	if err != nil {
		return nil, err
	}
	for _, region := range resp.Regions {
		regions = append(regions, *region.RegionName)
	}
	return regions, nil
}

// ParseRegion checks whether the provided region is valid. If an empty region is provided, returns all valid regions.
func ParseRegion(cfg *aws2.Config, region string) ([]string, error) {
	all, err := GetAll(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "failed retrieving list of regions")
	}
	allRegions := sets.NewString(all...)

	if region == "" {
		// return a sorted list
		return allRegions.List(), nil
	}

	if !allRegions.Has(region) {
		return nil, fmt.Errorf("invalid region: %s", region)
	}
	return []string{region}, nil
}
