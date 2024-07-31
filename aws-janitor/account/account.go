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

package account

import (
	"context"
	"fmt"
	"strings"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	iamv2 "github.com/aws/aws-sdk-go-v2/service/iam"
	stsv2 "github.com/aws/aws-sdk-go-v2/service/sts"
)

func GetAccount(cfg aws2.Config, region string) (string, error) {
	svc := iamv2.NewFromConfig(cfg, func(option *iamv2.Options) {
		if region != "" {
			option.Region = region
		}
	})
	resp, err := svc.GetUser(context.TODO(), nil)
	if err == nil {
		arn, err := parseARN(*resp.User.Arn)
		if err != nil {
			return "", err
		}
		return arn.account, nil
	}
	svc2 := stsv2.NewFromConfig(cfg)
	input := &stsv2.GetCallerIdentityInput{}
	result, err := svc2.GetCallerIdentity(context.TODO(), input)
	if err != nil {
		return "", err
	}
	return *result.Account, nil
}

func parseARN(s string) (*arn, error) {
	pieces := strings.Split(s, ":")
	if len(pieces) != 6 || pieces[0] != "arn" || pieces[1] != "aws" {
		return nil, fmt.Errorf("invalid AWS ARN %q", s)
	}
	var resourceType string
	var resource string
	res := strings.SplitN(pieces[5], "/", 2)
	if len(res) == 1 {
		resource = res[0]
	} else {
		resourceType = res[0]
		resource = res[1]
	}
	return &arn{
		partition:    pieces[1],
		service:      pieces[2],
		region:       pieces[3],
		account:      pieces[4],
		resourceType: resourceType,
		resource:     resource,
	}, nil
}

// ARNs (used for uniquifying within our previous mark file)

type arn struct {
	partition    string
	service      string
	region       string
	account      string
	resourceType string
	resource     string
}
