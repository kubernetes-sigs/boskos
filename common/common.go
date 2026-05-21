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
	"time"

	"sigs.k8s.io/boskos/simpleclient/common"
)

const (
	// Busy state defines a resource being used.
	Busy = common.Busy
	// Cleaning state defines a resource being cleaned
	Cleaning = common.Cleaning
	// Dirty state defines a resource that needs cleaning
	Dirty = common.Dirty
	// Free state defines a resource that is usable
	Free = common.Free
	// Leased state defines a resource being leased in order to make a new resource
	Leased = common.Leased
	// ToBeDeleted is used for resources about to be deleted, they will be verified by a cleaner which mark them as tombstone
	ToBeDeleted = common.ToBeDeleted
	// Tombstone is the state in which a resource can safely be deleted
	Tombstone = common.Tombstone
	// Other is used to agglomerate unspecified states for metrics reporting
	Other = common.Other
)

var (
	// KnownStates is the set of all known states, excluding "other".
	KnownStates = common.KnownStates
)

// UserData is a map of Name to user defined interface, serialized into a string
type UserData = common.UserData

// UserDataMap is the standard Map version of UserMap, it is used to ease UserMap creation.
type UserDataMap = common.UserDataMap

// LeasedResources is a list of resources name that used in order to create another resource by Mason
type LeasedResources = common.LeasedResources

// Duration is a wrapper around time.Duration that parses times in either
// 'integer number of nanoseconds' or 'duration string' formats and serializes
// to 'duration string' format.
type Duration = common.Duration

// Resource abstracts any resource type that can be tracked by boskos
type Resource = common.Resource

// ResourceEntry is resource config format defined from config.yaml
type ResourceEntry = common.ResourceEntry

// BoskosConfig defines config used by boskos server
type BoskosConfig = common.BoskosConfig

// Metric contains analytics about a specific resource type
type Metric = common.Metric

// NewMetric returns a new Metric struct.
func NewMetric(rtype string) Metric {
	return common.NewMetric(rtype)
}

// NewResource creates a new Boskos Resource.
func NewResource(name, rtype, state, owner string, t time.Time) Resource {
	return common.NewResource(name, rtype, state, owner, t)
}

// NewResourcesFromConfig parse the a ResourceEntry into a list of resources
func NewResourcesFromConfig(e ResourceEntry) []Resource {
	return common.NewResourcesFromConfig(e)
}

// UserDataFromMap returns a UserData from a map
func UserDataFromMap(m UserDataMap) *UserData {
	return common.UserDataFromMap(m)
}

// UserDataNotFound will be returned if requested resource does not exist.
type UserDataNotFound = common.UserDataNotFound

// ResourceByName helps sorting resources by name
type ResourceByName = common.ResourceByName

// CommaSeparatedStrings is used to parse comma separated string flag into a list of strings
type CommaSeparatedStrings = common.CommaSeparatedStrings

func ResourceTypeNotFoundMessage(rType string) string {
	return common.ResourceTypeNotFoundMessage(rType)
}
