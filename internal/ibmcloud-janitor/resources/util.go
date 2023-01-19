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

package resources

import (
	"net/url"

	"github.com/pkg/errors"
)

// pagingHelper is used while listing resources. It parses and fetches the start token for
// getting the next set of resources if nextURL is returned by func f.
// func f has start as a parameter and returns the following:
// isDone bool - true denotes that iterating is not needed as there is no next set of resources
// nextURL string - denotes a URL for a page of resources which is parsed for fetching start token for next iteration
// e error - will break and return error if e is not nil
func pagingHelper(f func(string) (bool, string, error)) (err error) {
	start := ""

	getStartToken := func(nextURL string) (string, error) {
		url, err := url.Parse(nextURL)
		if err != nil || url == nil {
			return "", errors.Wrapf(err, "failed to parse next url for getting next resources")
		}

		start := url.Query().Get("start")
		return start, nil
	}

	for {
		isDone, nextURL, e := f(start)

		if e != nil {
			err = e
			break
		}

		if isDone {
			break
		}

		if nextURL != "" {
			start, err = getStartToken(nextURL)
			if err != nil {
				break
			}
		} else {
			break
		}
	}

	return
}
