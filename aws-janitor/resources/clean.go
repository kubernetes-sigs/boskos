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
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/boskos/aws-janitor/regions"
)

// CleanAll cleans all of the resources for all of the regions visible to
// the provided AWS session.
func CleanAll(opts Options, region string) error {
	regionList, err := regions.ParseRegion(opts.Session, region)
	if err != nil {
		return err
	}
	logrus.Infof("Regions: %s", strings.Join(regionList, ", "))

	var errs []error

	for _, r := range regionList {
		opts.Region = r
		logger := logrus.WithField("options", opts)
		for _, typ := range RegionalTypeList {
			logger.Debugf("Cleaning resource type %T", typ)
			set, err := typ.ListAll(opts)
			if err != nil {
				// ignore errors for resources we do not have permissions to list
				if reqerr, ok := errors.Cause(err).(awserr.RequestFailure); ok {
					if reqerr.StatusCode() == http.StatusForbidden {
						logger.Debugf("Skipping resources of type %T, account does not have permission to list", typ)
						continue
					}
				}
				errs = append(errs, errors.Wrapf(err, "Failed to list resources of type %T", typ))
				continue
			}
			if err := typ.MarkAndSweep(opts, set); err != nil {
				errs = append(errs, errors.Wrapf(err, "Failed to mark and sweep resources of type %T", typ))
			}
		}
	}

	opts.Region = regions.Default
	for _, typ := range GlobalTypeList {
		set, err := typ.ListAll(opts)
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "Failed to list resources of type %T", typ))
			continue
		}
		if err := typ.MarkAndSweep(opts, set); err != nil {
			errs = append(errs, errors.Wrapf(err, "Failed to mark and sweep resources of type %T", typ))
		}
	}

	if len(errs) > 0 {
		return kerrors.NewAggregate(errs)
	}
	return nil
}
