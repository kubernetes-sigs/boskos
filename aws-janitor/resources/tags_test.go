/*
Copyright 2021 The Kubernetes Authors.

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

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
)

func TestMatchesTag(t *testing.T) {
	tm, err := TagMatcherForTags([]string{
		"onlyKey",
		"keyWithEquals=",
		"foo=1",
		"foo=2",
		"bar=abc",
	})
	if err != nil {
		t.Fatalf("unexpected error creating tag matcher: %v", err)
	}
	for _, tc := range []struct {
		Key         string
		Value       string
		ShouldMatch bool
	}{
		{
			Key:         "onlyKey",
			Value:       "some value",
			ShouldMatch: true,
		},
		{
			Key:         "onlyKey",
			Value:       "",
			ShouldMatch: true,
		},
		{
			Key:         "keyWithEquals=",
			Value:       "val",
			ShouldMatch: false,
		},
		{
			Key:         "keyWithEquals=",
			Value:       "",
			ShouldMatch: false,
		},
		{
			Key:         "keyWithEquals",
			Value:       "val",
			ShouldMatch: false,
		},
		{
			Key:         "keyWithEquals",
			Value:       "",
			ShouldMatch: true,
		},
		{
			Key:         "foo",
			Value:       "1",
			ShouldMatch: true,
		},
		{
			Key:         "foo",
			Value:       "2",
			ShouldMatch: true,
		},
		{
			Key:         "foo",
			Value:       "3",
			ShouldMatch: false,
		},
		{
			Key:         "bar",
			Value:       "abc",
			ShouldMatch: true,
		},
		{
			Key:         "bar",
			Value:       "",
			ShouldMatch: false,
		},
		{
			Key:         "bar",
			Value:       "xyz",
			ShouldMatch: false,
		},
	} {
		matches := tm.Matches(tc.Key, tc.Value)
		if matches != tc.ShouldMatch {
			t.Errorf("Tag {%s: %s}: matches: expected=%v, got=%v", tc.Key, tc.Value, tc.ShouldMatch, matches)
		}
	}
}

func TestManagedPerTags(t *testing.T) {
	// These tags and matchers aren't using values, since we test that in the other unit test.
	metasynTags := Tags{"foo": "", "bar": "", "baz": ""}
	tmBar, err := TagMatcherForTags([]string{"bar"})
	if err != nil {
		t.Fatalf("unexpected error creating tag matcher: %v", err)
	}
	colorTags := Tags{"red": "", "orange": "", "yellow": "", "green": "", "blue": "", "indigo": "", "violet": ""}
	tmRGB, err := TagMatcherForTags([]string{"red", "green", "blue"})
	if err != nil {
		t.Fatalf("unexpected error creating tag matcher: %v", err)
	}
	tmEmpty, err := TagMatcherForTags(nil)
	if err != nil {
		t.Fatalf("unexpected error creating tag matcher: %v", err)
	}

	for _, tc := range []struct {
		Desc         string
		Tags         Tags
		IncludeTags  TagMatcher
		ExcludeTags  TagMatcher
		ShouldManage bool
	}{
		{
			Desc:         "no tags, no matchers",
			Tags:         nil,
			IncludeTags:  tmEmpty,
			ExcludeTags:  tmEmpty,
			ShouldManage: true,
		},
		{
			Desc:         "no tags, empty include, set exclude",
			IncludeTags:  tmEmpty,
			ExcludeTags:  tmBar,
			ShouldManage: true,
		},
		{
			Desc:         "no tags, set include, empty exclude tags",
			IncludeTags:  tmRGB,
			ExcludeTags:  tmEmpty,
			ShouldManage: false,
		},
		{
			Desc:         "exclude by tag (no include)",
			Tags:         colorTags,
			IncludeTags:  tmEmpty,
			ExcludeTags:  tmRGB,
			ShouldManage: false,
		},
		{
			Desc:         "no exclude by tag (no include)",
			Tags:         colorTags,
			IncludeTags:  tmEmpty,
			ExcludeTags:  tmBar,
			ShouldManage: true,
		},
		{
			Desc:         "include by tag, single match (no exclude)",
			Tags:         metasynTags,
			IncludeTags:  tmBar,
			ExcludeTags:  tmEmpty,
			ShouldManage: true,
		},
		{
			Desc:         "include by tag, multi match (no exclude)",
			Tags:         colorTags,
			IncludeTags:  tmRGB,
			ExcludeTags:  tmEmpty,
			ShouldManage: true,
		},
		{
			Desc:         "no include by tag, multi match (missing tags, no exclude)",
			Tags:         Tags{"red": ""},
			IncludeTags:  tmRGB,
			ExcludeTags:  tmEmpty,
			ShouldManage: false,
		},
		{
			Desc:         "include by tag, mismatch exclude",
			Tags:         metasynTags,
			IncludeTags:  tmBar,
			ExcludeTags:  tmRGB,
			ShouldManage: true,
		},
		{
			Desc:         "include by tag, exclude by single matching",
			Tags:         Tags{"foo": "", "bar": "", "baz": "", "green": ""},
			IncludeTags:  tmBar,
			ExcludeTags:  tmRGB,
			ShouldManage: false,
		},
	} {
		opts := Options{
			IncludeTags: tc.IncludeTags,
			ExcludeTags: tc.ExcludeTags,
		}
		managed := opts.ManagedPerTags(tc.Tags)
		if managed != tc.ShouldManage {
			t.Errorf("\"%v\": managed: expected=%v, got=%v", tc.Desc, tc.ShouldManage, managed)
		}
	}
}

func TestIncrementalFetchTags(t *testing.T) {
	tagsMap := map[string]Tags{"a": nil, "b": nil, "c": nil, "d": nil, "e": nil}
	funcCalls := 0
	processedIDs := 0

	err := incrementalFetchTags(tagsMap, 2, func(ids []*string) error {
		funcCalls++
		if len(ids) > 2 {
			t.Errorf("invalid number of ids in function call: expected<=%d, got=%d", 2, len(ids))
		}
		for _, id := range ids {
			processedIDs++
			name := *id
			if _, ok := tagsMap[name]; !ok {
				t.Errorf("id not in tag map: %v", id)
				continue
			}
			if tagsMap[name] == nil {
				tagsMap[name] = make(Tags)
			}
			tagsMap[name].Add(aws2.String("seen"), aws2.String("true"))
		}
		return nil
	})
	if err != nil {
		t.Errorf("incrementalFetchTags: unexpected error: %v", err)
	}
	if funcCalls != 3 {
		t.Errorf("func called incorrect number of times: expected=%d, got=%d", 3, funcCalls)
	}
	if processedIDs != len(tagsMap) {
		t.Errorf("unexpected number of processed ids: expected=%d, got=%d", len(tagsMap), processedIDs)
	}
	for key, tags := range tagsMap {
		if len(tags) != 1 {
			t.Errorf("expected 1 tag for key %s, got tags=%v", key, tags)
		}
		if _, ok := tags["seen"]; !ok {
			t.Errorf("missing key 'seen' for key %s", key)
		}
	}
}
