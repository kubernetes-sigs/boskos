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

package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"sigs.k8s.io/prow/pkg/logrusutil"

	boskosClient "sigs.k8s.io/boskos/client"
	"sigs.k8s.io/boskos/common"
	"sigs.k8s.io/boskos/internal/ibmcloud-janitor/resources"
)

var (
	boskosURL    = flag.String("boskos-url", "", "Boskos URL")
	rTypes       common.CommaSeparatedStrings
	username     = flag.String("username", "", "Username used to access the Boskos server")
	passwordFile = flag.String("password-file", "", "The path to password file used to access the Boskos server")
	logLevel     = flag.String("log-level", "info", fmt.Sprintf("Log level is one of %v.", logrus.AllLevels))
	debug        = flag.Bool("debug", false, "Setting it to true allows logs for PowerVS client")
	ignoreAPIKey = flag.Bool("ignore-api-key", false, "Setting it to true will skip clean up and rotation of API keys")
	accountID    = flag.String("account-id", "", "ID of the IBM Cloud account")
)

const (
	sleepTime = time.Minute
)

func init() {
	flag.Var(&rTypes, "resource-type", "comma-separated list of resources need to be cleaned up")
}

//nolint:nestif
func run(boskos *boskosClient.Client) error {
	for {
		for _, resourceType := range rTypes {
			if res, err := boskos.Acquire(resourceType, common.Dirty, common.Cleaning); errors.Cause(err) == boskosClient.ErrNotFound {
				logrus.Info("no resource acquired. Sleeping.")
				time.Sleep(sleepTime)
				continue
			} else if err != nil {
				return errors.Wrap(err, "Couldn't retrieve resources from Boskos")
			} else {
				options := &resources.CleanupOptions{
					Resource:     res,
					Debug:        *debug,
					IgnoreAPIKey: *ignoreAPIKey,
				}
				if *accountID != "" {
					options.AccountID = accountID
				}
				if err := resources.CleanAll(options); err != nil {
					return errors.Wrapf(err, "Couldn't clean resource %q", res.Name)
				}
				if err := boskos.UpdateOne(res.Name, common.Cleaning, res.UserData); err != nil {
					return errors.Wrapf(err, "Failed to update resource %q", res.Name)
				}
				if err := boskos.ReleaseOne(res.Name, common.Free); err != nil {
					return errors.Wrapf(err, "Failed to release resoures %q", res.Name)
				}
				logrus.WithField("name", res.Name).Info("Released resource")
			}
		}
	}
}

func main() {
	logrusutil.ComponentInit()
	flag.Parse()

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.WithError(err).Fatal("invalid log level specified")
	}
	logrus.SetLevel(level)

	if len(rTypes) == 0 {
		logrus.Info("--resource-type is empty! Setting it to the defaults: powervs-service and vpc-service")
		rTypes = []string{"powervs-service", "vpc-service"}
	}

	boskos, err := boskosClient.NewClient("IBMCloudJanitor", *boskosURL, *username, *passwordFile)
	if err != nil {
		logrus.WithError(err).Fatal("unable to create a Boskos client")
	}
	if err := run(boskos); err != nil {
		logrus.WithError(err).Error("Janitor failure")
	}
}
