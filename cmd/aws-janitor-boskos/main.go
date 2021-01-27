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

package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/test-infra/prow/logrusutil"
	"sigs.k8s.io/boskos/aws-janitor/account"
	"sigs.k8s.io/boskos/aws-janitor/regions"
	"sigs.k8s.io/boskos/aws-janitor/resources"
	"sigs.k8s.io/boskos/client"
	"sigs.k8s.io/boskos/common"
	awsboskos "sigs.k8s.io/boskos/common/aws"
)

var (
	boskosURL          = flag.String("boskos-url", "http://boskos", "Boskos URL")
	rTypes             common.CommaSeparatedStrings
	username           = flag.String("username", "", "Username used to access the Boskos server")
	passwordFile       = flag.String("password-file", "", "The path to password file used to access the Boskos server")
	region             = flag.String("region", "", "The region to clean (otherwise defaults to all regions)")
	sweepCount         = flag.Int("sweep-count", 5, "Number of times to sweep the resources")
	sweepSleep         = flag.String("sweep-sleep", "30s", "The duration to pause between sweeps")
	sweepSleepDuration time.Duration
	logLevel           = flag.String("log-level", "info", fmt.Sprintf("Log level is one of %v.", logrus.AllLevels))
	dryRun             = flag.Bool("dry-run", false, "If set, don't delete any resources, only log what would be done")
	ttlTagKey          = flag.String("ttl-tag-key", "", "If set, allow resources to use a tag with this key to override TTL")

	excludeTags common.CommaSeparatedStrings
	includeTags common.CommaSeparatedStrings
	excludeTM   resources.TagMatcher
	includeTM   resources.TagMatcher
)

const (
	sleepTime = time.Minute
)

func init() {
	flag.Var(&rTypes, "resource-type", "comma-separated list of resources need to be cleaned up")
	flag.Var(&excludeTags, "exclude-tags",
		"Resources with any of these tags will not be managed by the janitor. Given as a comma-separated list of tags in key[=value] format; excluding the value will match any tag with that key. Keys can be repeated.")
	flag.Var(&includeTags, "include-tags",
		"Resources must include all of these tags in order to be managed by the janitor. Given as a comma-separated list of tags in key[=value] format; excluding the value will match any tag with that key. Keys can be repeated.")
}

func main() {
	logrusutil.ComponentInit()
	flag.Parse()

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.WithError(err).Fatal("invalid log level specified")
	}
	logrus.SetLevel(level)

	if d, err := time.ParseDuration(*sweepSleep); err != nil {
		sweepSleepDuration = time.Second * 30
	} else {
		sweepSleepDuration = d
	}

	if len(rTypes) == 0 {
		logrus.Info("--resource-type is empty! Setting it to default: aws-account")
		rTypes = []string{"aws-account"}
	}

	excludeTM, err = resources.TagMatcherForTags(excludeTags)
	if err != nil {
		logrus.Fatalf("Error parsing --exclude-tags: %v", err)
	}
	includeTM, err = resources.TagMatcherForTags(includeTags)
	if err != nil {
		logrus.Fatalf("Error parsing --include-tags: %v", err)
	}

	boskos, err := client.NewClient("AWSJanitor", *boskosURL, *username, *passwordFile)
	if err != nil {
		logrus.WithError(err).Fatal("unable to create a Boskos client")
	}
	if err := run(boskos); err != nil {
		logrus.WithError(err).Error("Janitor failure")
	}
}

func run(boskos *client.Client) error {
	for {
		for _, resourceType := range rTypes {
			if res, err := boskos.Acquire(resourceType, common.Dirty, common.Cleaning); errors.Cause(err) == client.ErrNotFound {
				logrus.Info("no resource acquired. Sleeping.")
				time.Sleep(sleepTime)
				continue
			} else if err != nil {
				return errors.Wrap(err, "Couldn't retrieve resources from Boskos")
			} else {
				logrus.WithField("name", res.Name).Info("Acquired resource")
				if err := cleanResource(res); err != nil {
					return errors.Wrapf(err, "Couldn't clean resource %q", res.Name)
				}
				if err := boskos.ReleaseOne(res.Name, common.Free); err != nil {
					return errors.Wrapf(err, "Failed to release resoures %q", res.Name)
				}
				logrus.WithField("name", res.Name).Info("Released resource")
			}
		}
	}
}

func cleanResource(res *common.Resource) error {
	val, err := awsboskos.GetAWSCreds(res)
	if err != nil {
		return errors.Wrapf(err, "Couldn't get AWS creds from %q", res.Name)
	}
	creds := credentials.NewStaticCredentialsFromCreds(val)
	s, err := session.NewSession(aws.NewConfig().WithCredentials(creds))
	if err != nil {
		return errors.Wrapf(err, "Failed to create AWS session")
	}
	acct, err := account.GetAccount(s, regions.Default)
	if err != nil {
		return errors.Wrap(err, "Failed retrieving account")
	}
	opts := resources.Options{
		Session:     s,
		Account:     acct,
		DryRun:      *dryRun,
		ExcludeTags: excludeTM,
		IncludeTags: includeTM,
		TTLTagKey:   *ttlTagKey,
	}

	logrus.WithField("name", res.Name).Info("beginning cleaning")
	start := time.Now()

	for i := 0; i < *sweepCount; i++ {
		if err := resources.CleanAll(opts, *region); err != nil {
			if i == *sweepCount-1 {
				logrus.WithError(err).Warningf("Failed to clean resource %q", res.Name)
			}
		}
		if i < *sweepCount-1 {
			time.Sleep(sweepSleepDuration)
		}
	}

	duration := time.Since(start)

	logrus.WithFields(logrus.Fields{"name": res.Name, "duration": duration.Seconds()}).Info("Finished cleaning")
	return nil
}
