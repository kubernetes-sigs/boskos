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
	"strings"

	"github.com/IBM/platform-services-go-sdk/globaltaggingv1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const scheduleTag = "no-schedule"

func CleanAll(options *CleanupOptions) error {
	list, err := listResources(options.Resource.Type)
	if err != nil {
		return errors.Wrapf(err, "Failed to fetch the list of resources of type %q", options.Resource.Type)
	}
	for _, resource := range list {
		err = resource.cleanup(options)
		if err != nil {
			return errors.Wrapf(err, "Failed to clean the resource %q of type %q", options.Resource.Name, options.Resource.Type)
		}
	}
	if options.IgnoreAPIKey {
		logrus.Infof("Skipping cleanup and rotation of API keys for resource %s of type %s", options.Resource.Name, options.Resource.Type)
		return nil
	}

	for _, resource := range CommonResources {
		err = resource.cleanup(options)
		if err != nil {
			return errors.Wrap(err, "Failed to clean the global resources")
		}
	}
	return nil
}

// CheckForNoScehduleTag checks if the resource type is of PowerVS
// and if the resource has a tag `no-schedule`. If tag is present,
// return true.
func CheckForNoScehduleTag(options *CleanupOptions) (bool, error) {
	if !strings.Contains(options.Resource.Type, "powervs") {
		logrus.Infof("Skipping the check for schedule eligibility for type %s as it is only supported for type 'powervs'", options.Resource.Type)
		return false, nil
	}

	client, err := NewPowerVSClient(options)
	if err != nil {
		return false, errors.Wrap(err, "failed to create powervs client")
	}

	workspaceDetails, err := client.GetWorkspaceDetails()
	if err != nil {
		return false, errors.Wrap(err, "failed to fetch workspace details")
	}
	logrus.WithField("name", options.Resource.Name).Info("Fetched workspace details to check for tags")

	taggingClient, err := NewTaggingClient()
	if err != nil {
		return false, errors.Wrap(err, "failed to create tagging client")
	}

	listOptions := taggingClient.NewListTagsOptions()
	listOptions.SetTagType(globaltaggingv1.AttachTagOptionsTagTypeUserConst)
	listOptions.SetAccountID(*options.AccountID)
	listOptions.SetAttachedTo(*workspaceDetails.Details.Crn)

	tagList, _, err := taggingClient.ListTags(listOptions)

	if err != nil {
		return false, errors.Wrap(err, "failed to list tags")
	}

	for _, tag := range tagList.Items {
		if tag.Name != nil && *tag.Name == scheduleTag {
			logrus.WithField("name", options.Resource.Name).Infof("Resource is tagged with %s", scheduleTag)
			return true, nil
		}
	}

	return false, nil
}
