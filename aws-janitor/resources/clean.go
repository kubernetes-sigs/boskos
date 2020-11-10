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
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/boskos/aws-janitor/account"
	"sigs.k8s.io/boskos/aws-janitor/regions"
)

// CleanAll cleans all of the resources for all of the regions visible to
// the provided AWS session.
func CleanAll(sess *session.Session, region string) error {
	acct, err := account.GetAccount(sess, regions.Default)
	if err != nil {
		return errors.Wrap(err, "Failed to retrieve account")
	}
	logrus.Debugf("Account: %s", acct)

	regionList, err := regions.ParseRegion(sess, region)
	if err != nil {
		return err
	}
	logrus.Infof("Regions: %s", strings.Join(regionList, ", "))

	var errs []error

	for _, r := range regionList {
		for _, typ := range RegionalTypeList {
			set, err := typ.ListAll(sess, acct, r)
			if err != nil {
				// ignore errors for resources we do not have permissions to list
				if reqerr, ok := errors.Cause(err).(awserr.RequestFailure); ok {
					if reqerr.StatusCode() == http.StatusForbidden {
						logrus.Debugf("Skipping resources of type %T, account does not have permission to list", typ)
						continue
					}
				}
				errs = append(errs, errors.Wrapf(err, "Failed to list resources of type %T", typ))
				continue
			}
			if err := typ.MarkAndSweep(sess, acct, r, set); err != nil {
				errs = append(errs, errors.Wrapf(err, "Failed to list resources of type %T", typ))
			}
		}
	}

	for _, typ := range GlobalTypeList {
		set, err := typ.ListAll(sess, acct, regions.Default)
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "Failed to list resources of type %T", typ))
			continue
		}
		if err := typ.MarkAndSweep(sess, acct, regions.Default, set); err != nil {
			errs = append(errs, errors.Wrapf(err, "Failed to list resources of type %T", typ))
		}
	}

	if len(errs) > 0 {
		return kerrors.NewAggregate(errs)
	}
	return nil
}
