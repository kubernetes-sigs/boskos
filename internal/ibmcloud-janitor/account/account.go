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

package account

import (
	"errors"
	"os"

	"github.com/IBM/go-sdk-core/v5/core"
)

const (
	// File path where API Key is stored
	APIKeyEnv = "IBMCLOUD_ENV_FILE"
)

// Returns an authenticator for IBM Cloud
func GetAuthenticator() (core.Authenticator, error) {
	fileName := os.Getenv(APIKeyEnv)
	if fileName == "" {
		return nil, errors.New("please set IBMCLOUD_ENV_FILE, it cannot be empty")
	}

	key, err := os.ReadFile(fileName)
	if err != nil {
		return nil, err
	}

	auth := &core.IamAuthenticator{
		ApiKey: string(key),
	}
	return auth, nil
}
