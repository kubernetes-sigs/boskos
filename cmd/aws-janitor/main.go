/*
Copyright 2016 The Kubernetes Authors.

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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/test-infra/prow/logrusutil"
	"sigs.k8s.io/boskos/aws-janitor/account"
	"sigs.k8s.io/boskos/aws-janitor/regions"
	"sigs.k8s.io/boskos/aws-janitor/resources"
	s3path "sigs.k8s.io/boskos/aws-janitor/s3"
)

var (
	maxTTL   = flag.Duration("ttl", 24*time.Hour, "Maximum time before attempting to delete a resource. Set to 0s to nuke all non-default resources.")
	region   = flag.String("region", "", "The region to clean (otherwise defaults to all regions)")
	path     = flag.String("path", "", "S3 path for mark data (required when -all=false)")
	cleanAll = flag.Bool("all", false, "Clean all resources (ignores -path)")
	logLevel = flag.String("log-level", "info", fmt.Sprintf("Log level is one of %v.", logrus.AllLevels))
)

func main() {
	logrusutil.ComponentInit()
	flag.Parse()

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.WithError(err).Fatal("invalid log level specified")
	}
	logrus.SetLevel(level)

	// Retry aggressively (with default back-off). If the account is
	// in a really bad state, we may be contending with API rate
	// limiting and fighting against the very resources we're trying
	// to delete.
	sess := session.Must(session.NewSessionWithOptions(session.Options{Config: aws.Config{MaxRetries: aws.Int(100)}}))

	if *cleanAll {
		if err := resources.CleanAll(sess, *region); err != nil {
			logrus.Fatalf("Error cleaning all resources: %v", err)
		}
	} else if err := markAndSweep(sess, *region); err != nil {
		logrus.Fatalf("Error marking and sweeping resources: %v", err)
	}
}

func markAndSweep(sess *session.Session, region string) error {
	s3p, err := s3path.GetPath(sess, *path)
	if err != nil {
		return errors.Wrapf(err, "-path %q isn't a valid S3 path", *path)
	}

	acct, err := account.GetAccount(sess, regions.Default)
	if err != nil {
		return errors.Wrap(err, "Error getting current user")
	}
	logrus.Debugf("account: %s", acct)

	var regionList []string
	if region == "" {
		regionList, err = regions.GetAll(sess)
		if err != nil {
			return errors.Wrap(err, "Error getting available regions")
		}
	} else {
		regionList = []string{region}
	}
	logrus.Infof("Regions: %+v", regionList)

	res, err := resources.LoadSet(sess, s3p, *maxTTL)
	if err != nil {
		return errors.Wrapf(err, "Error loading %q", *path)
	}

	for _, region := range regionList {
		for _, typ := range resources.RegionalTypeList {
			if err := typ.MarkAndSweep(sess, acct, region, res); err != nil {
				return errors.Wrapf(err, "Error sweeping %T", typ)
			}
		}
	}

	for _, typ := range resources.GlobalTypeList {
		if err := typ.MarkAndSweep(sess, acct, regions.Default, res); err != nil {
			return errors.Wrapf(err, "Error sweeping %T", typ)
		}
	}

	swept := res.MarkComplete()
	if err := res.Save(sess, s3p); err != nil {
		return errors.Wrapf(err, "Error saving %q", *path)
	}

	logrus.Infof("swept %d resources", swept)

	return nil
}
