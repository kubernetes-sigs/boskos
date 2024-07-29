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

package aws

import (
	"errors"
	"fmt"
	"strings"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"

	"sigs.k8s.io/boskos/common"
)

const (
	// UserDataAccessIDKey is the key in UserData containing the AWS Access key ID
	UserDataAccessIDKey = "access-key-id"

	// UserDataSecretAccessKey is the key in UserData containing the AWS Secret access key
	UserDataSecretAccessKey = "secret-access-key"
)

// GetAWSCreds tries to fetch AWS credentials from a resource
func GetAWSCreds(r *common.Resource) (aws2.Credentials, error) {
	val := aws2.Credentials{}

	if !strings.HasSuffix(r.Type, "aws-account") {
		return val, fmt.Errorf("invalid aws resource type %q", r.Type)
	}

	accessKey, ok := r.UserData.Map.Load(UserDataAccessIDKey)
	if !ok {
		return val, errors.New("No Access Key ID in UserData")
	}
	secretKey, ok := r.UserData.Map.Load(UserDataSecretAccessKey)
	if !ok {
		return val, errors.New("No Secret Access Key in UserData")
	}

	val.AccessKeyID = accessKey.(string)
	val.SecretAccessKey = secretKey.(string)

	return val, nil
}
