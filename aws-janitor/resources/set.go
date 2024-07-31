/*
Copyright 2019 The Kubernetes Authors.

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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	s3v2 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3v2types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/sirupsen/logrus"

	s3path "sigs.k8s.io/boskos/aws-janitor/s3"
)

// Set keeps track of the first time we saw a particular
// ARN, and the global TTL. See Mark() for more details.
type Set struct {
	firstSeen map[string]time.Time // ARN -> first time we saw
	marked    map[string]bool      // ARN -> seen this run
	swept     []string             // List of resources we attempted to sweep (to summarize)
	ttl       time.Duration
}

func NewSet(ttl time.Duration) *Set {
	return &Set{
		firstSeen: make(map[string]time.Time),
		marked:    make(map[string]bool),
		ttl:       ttl,
	}
}

func (s *Set) GetARNs() []string {
	slice := make([]string, len(s.firstSeen))
	i := 0
	for key := range s.firstSeen {
		slice[i] = key
		i++
	}

	sort.Strings(slice)
	return slice
}

func LoadSet(config *aws2.Config, p *s3path.Path, ttl time.Duration) (*Set, error) {
	s := NewSet(ttl)
	svc := s3v2.NewFromConfig(*config, func(opt *s3v2.Options) {
		opt.Region = p.Region
	})

	resp, err := svc.GetObject(context.TODO(), &s3v2.GetObjectInput{Bucket: aws2.String(p.Bucket), Key: aws2.String(p.Key)})
	if err != nil {
		var noSuchKey *s3v2types.NoSuchKey
		var apiErr smithy.APIError
		if errors.As(err, &noSuchKey) {
			return s, nil
		} else if errors.As(err, &apiErr) {
			if apiErr.ErrorCode() == "NoSuchKey" {
				return s, nil
			}
		}
		return nil, err
	}

	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&s.firstSeen); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Set) Save(config *aws2.Config, p *s3path.Path) error {
	b, err := json.MarshalIndent(s.firstSeen, "", "  ")
	if err != nil {
		return err
	}

	svc := s3v2.NewFromConfig(*config, func(opt *s3v2.Options) {
		opt.Region = p.Region
	})

	_, err = svc.PutObject(context.TODO(),
		&s3v2.PutObjectInput{
			Bucket:       aws2.String(p.Bucket),
			Key:          aws2.String(p.Key),
			Body:         bytes.NewReader(b),
			CacheControl: aws2.String("max-age=0"),
		})

	return err
}

// Mark marks a particular resource as currently present, records when it was
// created or first seen, and advises on whether it should be deleted.
//
// When determining whether a resource should be deleted, first the options for
// IncludeTags and ExcludeTags are applied against the provided tags.
// If the resource should be managed per tags, then the TTL is evaluated.
// Note that if the TTLTagKey option is set, the resource has a tag matching this key,
// and the global TTL is not set to 0, then the TTL duration in this tag's value will
// be used for this resource.
//
// If Mark(r) returns true, the resource is managed per tags, and the TTL has expired
// for r and it should be deleted.
// If the created time is not provided, the current time is used instead.
func (s *Set) Mark(opts Options, r Interface, created *time.Time, tags Tags) bool {
	key := r.ResourceKey()
	s.marked[key] = true

	// Calculate the most likely creation time.  If a creation time is explicitly passed to us, use that.
	// Otherwise, we either use a previously recorded timestamp or record and use the current time.
	now := time.Now()

	// Use the created time if it is valid
	var firstSeen time.Time
	if created != nil && created.Before(now) && !created.IsZero() && !created.Equal(time.Unix(0, 0)) {
		firstSeen = *created
	} else {
		if t, ok := s.firstSeen[key]; ok {
			firstSeen = t
		} else {
			firstSeen = now
		}
	}

	s.firstSeen[key] = firstSeen

	if !opts.ManagedPerTags(tags) {
		return false
	}

	perResourceTTL := s.ttl
	if opts.TTLTagKey != "" {
		if val, ok := tags[opts.TTLTagKey]; ok {
			tagTTL, err := time.ParseDuration(val)
			if err != nil {
				logrus.Errorf("resource %s: invalid duration '%s' in tag '%s': %v", r.ResourceKey(), val, opts.TTLTagKey, err)
			} else {
				perResourceTTL = tagTTL
				logrus.Debugf("resource %s: TTL set to %v by tag", r.ResourceKey(), perResourceTTL)
			}
		}
	}

	// If the global TTL is 0, the resource should be deleted now. (This cannot be overridden by tags.)
	if s.ttl == 0 || now.Sub(firstSeen) > perResourceTTL {
		s.swept = append(s.swept, key)
		return true
	}
	return false
}

// MarkComplete figures out which ARNs were in previous passes but not
// this one, and eliminates them. It should only be run after all
// resources have been marked.
func (s *Set) MarkComplete() int {
	var gone []string
	for key := range s.firstSeen {
		if !s.marked[key] {
			gone = append(gone, key)
		}
	}

	for _, key := range gone {
		logrus.Debugf("%s: deleted since last run", key)
		delete(s.firstSeen, key)
	}

	if len(s.swept) > 0 {
		logrus.Errorf("%d resources swept: %v", len(s.swept), s.swept)
	}

	return len(s.swept)
}
