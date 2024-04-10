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

package common

import (
	"io/ioutil"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseResourceTypesFromConfig(t *testing.T) {
	tmp := t.TempDir()
	for _, tc := range []struct {
		name          string
		config        string
		wantResources []string
	}{
		{
			name: "parse resources properly",
			config: `resources:
- type: t1
  names:
  - t1-n1
  - t1-n2
  state: free
- type: t2
  names:
  - t2-n1
  - t2-n2
  state: free`,
			wantResources: []string{"t1", "t2"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			f, err := ioutil.TempFile(tmp, "boskos-config*.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(tc.config); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			types, err := parseResourceTypesFromConfig(f.Name())
			if err != nil {
				t.Fatalf("parse resources: %s", err.Error())
			}
			if diff := cmp.Diff(tc.wantResources, types); diff != "" {
				t.Errorf("resource types don't match: %s", diff)
			}
		})
	}
}
