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

	"sigs.k8s.io/boskos/common"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	// DRLCType is the DynamicResourceLifeCycle CRD type
	DRLCType = Type{
		Kind:       reflect.TypeOf(DRLCObject{}).Name(),
		ListKind:   reflect.TypeOf(DRLCObjectList{}).Name(),
		Singular:   "dynamicresourcelifecycle",
		Plural:     "dynamicresourcelifecycles",
		Object:     &DRLCObject{},
		Collection: &DRLCObjectList{},
	}
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DRLCObject holds generalized configuration information about how the
// resource needs to be created.
// Some Resource might not have a ResourcezConfig (Example Project)
type DRLCObject struct {
	v1.TypeMeta   `json:",inline"`
	v1.ObjectMeta `json:"metadata,omitempty"`
	Spec          DRLCSpec `json:"spec"`
}

// DRLCSpec holds config implementation specific configuration as well as resource needs
type DRLCSpec struct {
	InitialState string               `json:"state"`
	MaxCount     int                  `json:"max-count"`
	MinCount     int                  `json:"min-count"`
	LifeSpan     *time.Duration       `json:"lifespan,omitempty"`
	Config       common.ConfigType    `json:"config"`
	Needs        common.ResourceNeeds `json:"needs"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DRLCObjectList implements the Collections interface
type DRLCObjectList struct {
	v1.TypeMeta `json:",inline"`
	v1.ListMeta `json:"metadata,omitempty"`
	Items       []DRLCObject `json:"items"`
}

// GetName implements the Object interface
func (in *DRLCObject) GetName() string {
	return in.Name
}

func (in *DRLCObject) ToDynamicResourceLifeCycle() common.DynamicResourceLifeCycle {
	return common.DynamicResourceLifeCycle{
		Type:         in.Name,
		InitialState: in.Spec.InitialState,
		MinCount:     in.Spec.MinCount,
		MaxCount:     in.Spec.MaxCount,
		LifeSpan:     in.Spec.LifeSpan,
		Config:       in.Spec.Config,
		Needs:        in.Spec.Needs,
	}
}

// FromDynamicResourceLifecycle converts a common.DynamicResourceLifeCycle into a *DRLCObject
func FromDynamicResourceLifecycle(r common.DynamicResourceLifeCycle) *DRLCObject {
	return &DRLCObject{
		ObjectMeta: v1.ObjectMeta{
			Name: r.Type,
		},
		Spec: DRLCSpec{
			InitialState: r.InitialState,
			MinCount:     r.MinCount,
			MaxCount:     r.MaxCount,
			LifeSpan:     r.LifeSpan,
			Config:       r.Config,
			Needs:        r.Needs,
		},
	}
}
