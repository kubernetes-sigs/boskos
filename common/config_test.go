/*
Copyright 2020 The Kubernetes Authors.

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
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestValidateConfig(t *testing.T) {
	testCases := []struct {
		name           string
		in             *BoskosConfig
		expectedErrMsg string
	}{
		{
			name: "Resource name has uppercase letters",
			in: &BoskosConfig{Resources: []ResourceEntry{
				{
					Names: []string{"openstack-OSUOSL-01", "openstack-OSUOSL-02", "openstack-OSUOSL-03"},
					State: "free",
					Type:  "openstack",
				},
			}},
			expectedErrMsg: `[.0.names.0(openstack-OSUOSL-01) is invalid: [a DNS-1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')], .0.names.1(openstack-OSUOSL-02) is invalid: [a DNS-1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')], .0.names.2(openstack-OSUOSL-03) is invalid: [a DNS-1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')]]`,
		},
		{
			name: "Duplicate resource",
			in: &BoskosConfig{Resources: []ResourceEntry{
				{
					Names: []string{"openstack-01", "openstack-01"},
					State: "free",
					Type:  "openstack",
				},
			}},
			expectedErrMsg: ".0.names.1(openstack-01) is a duplicate",
		},
		{
			name: "Empty type",
			in: &BoskosConfig{Resources: []ResourceEntry{
				{
					Names: []string{"openstack-01"},
					State: "free",
				},
			}},
			expectedErrMsg: ".0.type: must be set",
		},
		{
			name: "Max count is zero",
			in: &BoskosConfig{Resources: []ResourceEntry{
				{
					State:    "free",
					MaxCount: 0,
				},
			}},
			expectedErrMsg: "[.0.type: must be set, .0.max-count: must be >0]",
		},
		{
			name: "Min is < max",
			in: &BoskosConfig{Resources: []ResourceEntry{
				{
					State:    "free",
					MaxCount: 1,
					MinCount: 2,
				},
			}},
			expectedErrMsg: "[.0.type: must be set, .0.min-count: must be <= .0.max-count]",
		},
		{
			name: "Resource is both static and dynamic",
			in: &BoskosConfig{Resources: []ResourceEntry{{
				State:    "free",
				Type:     "some-type",
				MaxCount: 1,
				MinCount: 1,
				Names:    []string{"my-resource"},
			}}},
			expectedErrMsg: "[.0.min-count must be unset when the names property is set, .0.max-count must be unset when the names property is set]",
		},
	}

	for _, tc := range testCases {
		// appease the linter
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var errMsg string
			if err := ValidateConfig(tc.in); err != nil {
				errMsg = err.Error()
			}

			if diff := cmp.Diff(tc.expectedErrMsg, errMsg); diff != "" {
				t.Errorf("actual error doesn't match expected: %s", diff)
			}
		})
	}
}
