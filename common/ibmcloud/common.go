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
	ResourceGroup     = "resource-group"
)

type PowerVSResourceData struct {
	ServiceInstanceID string
	Zone              string
}

type VPCResourceData struct {
	Region        string
	ResourceGroup string
	VPCID         string
}

// Fetches the resource user data for type powervs-service
func GetPowerVSResourceData(r *common.Resource) (*PowerVSResourceData, error) {
	if !strings.HasPrefix(r.Type, "powervs") {
		return nil, fmt.Errorf("invalid resource type %q", r.Type)
	}

	sid, ok := r.UserData.Map.Load(ServiceInstanceID)
	if !ok {
		return nil, errors.New("no Service Instance ID in UserData")
	}

	zone, ok := r.UserData.Map.Load(Zone)
	if !ok {
		return nil, errors.New("no zone in UserData")
	}

	return &PowerVSResourceData{
		ServiceInstanceID: sid.(string),
		Zone:              zone.(string),
	}, nil
}

// Fetches the resource user data for type vpc-service
func GetVPCResourceData(r *common.Resource) (*VPCResourceData, error) {
	if !strings.HasPrefix(r.Type, "vpc") {
		return nil, fmt.Errorf("invalid resource type %q", r.Type)
	}

	region, ok := r.UserData.Map.Load(Region)
	if !ok {
		return nil, errors.New("no region in UserData")
	}
	rg, ok := r.UserData.Map.Load(ResourceGroup)
	if !ok {
		return nil, errors.New("no resource group in UserData")
	}
	data := &VPCResourceData{
		Region:        region.(string),
		ResourceGroup: rg.(string),
	}

	// Optional VPC ID
	if vpcID, ok := r.UserData.Map.Load("vpc-id"); ok {
		data.VPCID = vpcID.(string)
	}

	return data, nil
}

// Updates user data of the resource
func UpdateResource(r *common.Resource, apikey string) {
	r.UserData.Store(APIKey, apikey)
}
