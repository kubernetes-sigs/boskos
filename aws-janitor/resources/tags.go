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
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"k8s.io/apimachinery/pkg/util/sets"
)

type Tag struct {
	Key   string
	Value string
}

func NewTag(key *string, value *string) Tag {
	return Tag{
		Key:   aws.StringValue(key),
		Value: aws.StringValue(value),
	}
}

// TagMatcher maps keys to valid values. An empty set of values will result in matching tags with any value.
type TagMatcher map[string]sets.String

// TagMatcherForTags creates a new TagMatcher for the given list of tags provided in key=value format.
// If "=value" is not provided, then the TagMatcher will match any value for that key.
// (If the value is empty, only an empty tag value matches.)
func TagMatcherForTags(tags []string) (TagMatcher, error) {
	tm := make(TagMatcher)
	for _, tag := range tags {
		parts := strings.SplitN(tag, "=", 2)
		if len(parts) < 1 {
			return nil, fmt.Errorf("invalid tag: %q", tag)
		}
		key := parts[0]
		if _, ok := tm[key]; !ok {
			tm[key] = sets.NewString()
		}
		if len(parts) == 2 {
			tm[key].Insert(parts[1])
		}
	}

	return tm, nil
}

func (tm TagMatcher) Matches(tag Tag) bool {
	vals, ok := tm[tag.Key]
	if !ok {
		// No tag matcher for this key
		return false
	}
	if vals.Len() == 0 {
		// This matcher matches all values for a given key.
		return true
	}
	return vals.Has(tag.Value)
}

// ManagedPerTags returns whether the given list of tags is matched by all IncludeTags and no ExcludeTags.
func (opts Options) ManagedPerTags(tags []Tag) bool {
	included := 0
	for _, t := range tags {
		if opts.ExcludeTags.Matches(t) {
			return false
		}
		if opts.IncludeTags.Matches(t) {
			included++
		}
	}
	return included == len(opts.IncludeTags)
}

func fromEC2Tags(ec2tags []*ec2.Tag) []Tag {
	tags := make([]Tag, 0, len(ec2tags))
	for _, ec2t := range ec2tags {
		tags = append(tags, NewTag(ec2t.Key, ec2t.Value))
	}
	return tags
}

// incrementalFetchTags creates slices of IDs from tagMap no longer than inc at a time, calling
// f with these slices.
// This is intended to be used with APIs that allow querying for tags of a limited number of multiple resources at once.
// If f errors, incrementalFetchTags returns early with the error.
func incrementalFetchTags(tagMap map[string][]Tag, inc int, f func([]*string) error) error {
	ids := make([]*string, 0, len(tagMap))
	for id := range tagMap {
		ids = append(ids, aws.String(id))
	}

	for start := 0; start < len(ids); start += inc {
		end := start + inc
		if end > len(ids) {
			end = len(ids)
		}
		err := f(ids[start:end])
		if err != nil {
			return err
		}
	}
	return nil
}
