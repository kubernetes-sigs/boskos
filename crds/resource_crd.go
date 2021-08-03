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

package crds

import (
	"reflect"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/boskos/common"
)

var (
	// ResourceType is the ResourceObject CRD type
	ResourceType = Type{
		Kind:       reflect.TypeOf(ResourceObject{}).Name(),
		ListKind:   reflect.TypeOf(ResourceObjectList{}).Name(),
		Singular:   "resource",
		Plural:     "resources",
		Object:     &ResourceObject{},
		Collection: &ResourceObjectList{},
	}
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ResourceObject represents common.ResourceObject. It implements the Object interface.
type ResourceObject struct {
	v1.TypeMeta   `json:",inline"`
	v1.ObjectMeta `json:"metadata,omitempty"`
	Spec          ResourceSpec   `json:"spec,omitempty"`
	Status        ResourceStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ResourceObjectList is the Collection implementation
type ResourceObjectList struct {
	v1.TypeMeta `json:",inline"`
	v1.ListMeta `json:"metadata,omitempty"`
	Items       []ResourceObject `json:"items"`
}

// ResourceSpec holds information that are not likely to change
type ResourceSpec struct {
	Type string `json:"type"`
}

// ResourceStatus holds information that are likely to change
type ResourceStatus struct {
	State          string            `json:"state,omitempty"`
	Owner          string            `json:"owner"`
	LastUpdate     v1.Time           `json:"lastUpdate,omitempty"`
	UserData       map[string]string `json:"userData,omitempty"`
	ExpirationDate *v1.Time          `json:"expirationDate,omitempty"`
}

// ToResource returns the common.Resource representation for
// a ResourceObject
func (in *ResourceObject) ToResource() common.Resource {
	return common.Resource{
		Name:           in.Name,
		Type:           in.Spec.Type,
		Owner:          in.Status.Owner,
		State:          in.Status.State,
		LastUpdate:     in.Status.LastUpdate.Time,
		UserData:       common.UserDataFromMap(in.Status.UserData),
		ExpirationDate: metaTimeToTime(in.Status.ExpirationDate),
	}
}

func metaTimeToTime(in *v1.Time) *time.Time {
	if in == nil {
		return nil
	}
	return &in.Time
}

// FromResource converts a common.Resource to a *ResourceObject
func FromResource(r common.Resource) *ResourceObject {
	if r.UserData == nil {
		r.UserData = &common.UserData{}
	}
	return &ResourceObject{
		ObjectMeta: v1.ObjectMeta{
			Name: r.Name,
		},
		Spec: ResourceSpec{
			Type: r.Type,
		},
		Status: ResourceStatus{
			Owner:          r.Owner,
			State:          r.State,
			LastUpdate:     v1.Time{Time: r.LastUpdate},
			UserData:       map[string]string(r.UserData.ToMap()),
			ExpirationDate: timeToMetaTime(r.ExpirationDate),
		},
	}
}

func timeToMetaTime(in *time.Time) *v1.Time {
	if in == nil {
		return nil
	}
	return &v1.Time{Time: *in}
}

// NewResource creates a new Boskos Resource.
func NewResource(name, rtype, state, owner string, t v1.Time) *ResourceObject {
	// If no state defined, mark as Free
	if state == "" {
		state = common.Free
	}

	return &ResourceObject{
		ObjectMeta: v1.ObjectMeta{
			Name: name,
		},
		Spec: ResourceSpec{
			Type: rtype,
		},
		Status: ResourceStatus{
			State:      state,
			Owner:      owner,
			LastUpdate: t,
			UserData:   map[string]string{},
		},
	}
}
