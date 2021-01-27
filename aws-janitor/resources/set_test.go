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

package resources

import (
	"testing"
	"time"
)

type fakeResource struct {
	Name string
}

func (f fakeResource) ARN() string {
	return f.Name
}

func (f fakeResource) ResourceKey() string {
	return f.Name
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func TestMarkCreationTimes(t *testing.T) {
	for _, deleteAll := range []bool{false, true} {
		ttl := time.Hour

		now := time.Now()
		sixHoursAgo := now.Add(-6 * time.Hour)
		threeHoursAgo := now.Add(-3 * time.Hour)
		tenMinutesAgo := now.Add(-10 * time.Minute)
		epoch := time.Unix(0, 0)
		unset := time.Time{}

		if deleteAll {
			ttl = 0
		}
		s := NewSet(ttl)

		prevSeenNotExpired := fakeResource{Name: "PreviouslySeenNotExpired"}
		s.firstSeen[prevSeenNotExpired.ResourceKey()] = tenMinutesAgo
		prevSeenExpired := fakeResource{Name: "PreviouslySeenExpired"}
		s.firstSeen[prevSeenExpired.ResourceKey()] = threeHoursAgo
		prevSeenNewOlderCreateTime := fakeResource{Name: "PreviouslySeenNewOlderCreateTime"}
		s.firstSeen[prevSeenNewOlderCreateTime.ResourceKey()] = tenMinutesAgo
		prevSeenNewYoungerCreateTime := fakeResource{Name: "PreviouslySeenNewYoungerCreateTime"}
		s.firstSeen[prevSeenNewYoungerCreateTime.ResourceKey()] = threeHoursAgo

		for _, tc := range []struct {
			Resource          fakeResource
			ShouldDelete      bool
			CreateTime        *time.Time
			ExpectedFirstSeen time.Time
			Tags              []Tag
		}{
			{
				// New resource, no creation time -> should use time.Now()
				Resource:          fakeResource{"NoCreateTime"},
				ShouldDelete:      false,
				CreateTime:        nil,
				ExpectedFirstSeen: now,
			},
			{
				Resource:          fakeResource{"EpochCreateTime"},
				ShouldDelete:      false,
				CreateTime:        &epoch,
				ExpectedFirstSeen: now,
			},
			{
				Resource:          fakeResource{"UnsetCreateTime"},
				ShouldDelete:      false,
				CreateTime:        &unset,
				ExpectedFirstSeen: now,
			},
			{
				Resource:          fakeResource{"AlwaysDelete"},
				ShouldDelete:      true,
				CreateTime:        &sixHoursAgo,
				ExpectedFirstSeen: sixHoursAgo,
			},
			{
				Resource:          prevSeenNotExpired,
				ShouldDelete:      false,
				CreateTime:        nil,
				ExpectedFirstSeen: tenMinutesAgo,
			},
			{
				Resource:          prevSeenExpired,
				ShouldDelete:      true,
				CreateTime:        nil,
				ExpectedFirstSeen: threeHoursAgo,
			},
			{
				// If running against an older database, we want to ensure it updates with our new knowledge.
				Resource:          prevSeenNewOlderCreateTime,
				ShouldDelete:      true,
				CreateTime:        &threeHoursAgo,
				ExpectedFirstSeen: threeHoursAgo,
			},
			{
				// Some resources only have times that can potentially reset to newer times (e.g. startup date)
				Resource:          prevSeenNewYoungerCreateTime,
				ShouldDelete:      true,
				CreateTime:        &tenMinutesAgo,
				ExpectedFirstSeen: threeHoursAgo,
			},
		} {
			shouldDelete := deleteAll || tc.ShouldDelete
			opts := Options{}
			delete := s.Mark(opts, tc.Resource, tc.CreateTime, tc.Tags)
			if delete != shouldDelete {
				t.Errorf("%s: delete: expected=%v, got=%v", tc.Resource.Name, shouldDelete, delete)
			}

			found := false
			for _, key := range s.swept {
				if key == tc.Resource.ResourceKey() {
					found = true
					break
				}
			}
			if found != shouldDelete {
				t.Errorf("%s: resource not found in swept list", tc.Resource.Name)
			}

			if _, ok := s.marked[tc.Resource.ResourceKey()]; !ok {
				t.Errorf("%s: not marked", tc.Resource.Name)
			}

			firstSeen := s.firstSeen[tc.Resource.ResourceKey()]
			// Give a little leeway for varying values of Time.Now()
			if absDuration(tc.ExpectedFirstSeen.Sub(firstSeen)) > time.Minute {
				t.Errorf("%s: firstSeen: expected=%v, got=%v", tc.Resource.Name, tc.ExpectedFirstSeen, firstSeen)
			}
		}
	}
}
