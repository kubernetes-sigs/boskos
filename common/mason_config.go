/*
Copyright 2017 The Kubernetes Authors.

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

package common

import (
	"sigs.k8s.io/boskos/simpleclient/common"
)

// ResourceNeeds maps the type to count of resources types needed
type ResourceNeeds = common.ResourceNeeds

// TypeToResources stores all the leased resources with the same type f
type TypeToResources = common.TypeToResources

// ConfigType gather the type of config to be applied by Mason in order to construct the resource
type ConfigType = common.ConfigType

// DynamicResourceLifeCycle defines the life cycle of a dynamic resource.
// All Resource of a given type will be constructed using the same configuration
type DynamicResourceLifeCycle = common.DynamicResourceLifeCycle

// DRLCByName helps sorting ResourcesConfig by name
type DRLCByName = common.DRLCByName

// NewDynamicResourceLifeCycleFromConfig parse the a ResourceEntry into a DynamicResourceLifeCycle
func NewDynamicResourceLifeCycleFromConfig(e ResourceEntry) DynamicResourceLifeCycle {
	return common.NewDynamicResourceLifeCycleFromConfig(e)
}

// GenerateDynamicResourceName generates a unique name for dynamic resources
func GenerateDynamicResourceName() string {
	return common.GenerateDynamicResourceName()
}
