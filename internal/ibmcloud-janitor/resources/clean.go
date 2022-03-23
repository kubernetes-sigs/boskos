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
	"github.com/pkg/errors"
)

func CleanAll(options *CleanupOptions) error {
	list, err := listResources(options.Resource.Type)
	if err != nil {
		return errors.Wrapf(err, "Failed to fetch the list of resources of type %q", options.Resource.Name)
	}
	for _, resource := range list {
		err = resource.cleanup(options)
		if err != nil {
			return errors.Wrapf(err, "Failed to clean the resources of type %q", options.Resource.Type)
		}
	}

	for _, resource := range CommonResources {
		err = resource.cleanup(options)
		if err != nil {
			return errors.Wrap(err, "Failed to clean the global resources")
		}
	}
	return nil
}
