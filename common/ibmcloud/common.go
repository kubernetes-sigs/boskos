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

package ibmcloud

import (
	"errors"
	"fmt"
	"strings"

	"sigs.k8s.io/boskos/common"
)

const (
	ServiceInstanceID = "service-instance-id"
	APIKey            = "api-key"
	Region            = "region"
	Zone              = "zone"
)

type ResourceData struct {
	ServiceInstanceID string
	APIKey            string
	Region            string
	Zone              string
}

// Fetches the resource user data
func GetResourceData(r *common.Resource) (*ResourceData, error) {
	if !strings.HasPrefix(r.Type, "powervs") {
		return nil, fmt.Errorf("invalid resource type %q", r.Type)
	}

	sid, ok := r.UserData.Map.Load(ServiceInstanceID)
	if !ok {
		return nil, errors.New("no Service Instance ID in UserData")
	}

	key, ok := r.UserData.Map.Load(APIKey)
	if !ok {
		return nil, errors.New("no API key in UserData")
	}

	region, ok := r.UserData.Map.Load(Region)
	if !ok {
		return nil, errors.New("no region in UserData")
	}

	zone, ok := r.UserData.Map.Load(Zone)
	if !ok {
		return nil, errors.New("no zone in UserData")
	}

	return &ResourceData{
		ServiceInstanceID: sid.(string),
		APIKey:            key.(string),
		Region:            region.(string),
		Zone:              zone.(string),
	}, nil
}

// Updates user data of the resource
func UpdateResource(r *common.Resource, apikey string) {
	r.UserData.Store(APIKey, apikey)
}
