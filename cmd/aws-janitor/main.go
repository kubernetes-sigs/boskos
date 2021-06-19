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
	"os"
	"runtime"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/sirupsen/logrus"

	"k8s.io/test-infra/prow/logrusutil"

	"sigs.k8s.io/boskos/aws-janitor/account"
	"sigs.k8s.io/boskos/aws-janitor/regions"
	"sigs.k8s.io/boskos/aws-janitor/resources"
	s3path "sigs.k8s.io/boskos/aws-janitor/s3"
	"sigs.k8s.io/boskos/common"
)

var (
	maxTTL      = flag.Duration("ttl", 24*time.Hour, "Maximum time before attempting to delete a resource. Set to 0s to nuke all non-default resources.")
	region      = flag.String("region", "", "The region to clean (otherwise defaults to all regions)")
	path        = flag.String("path", "", "S3 path for mark data (required when -all=false)")
	cleanAll    = flag.Bool("all", false, "Clean all resources (ignores -path and -ttl)")
	oneShot     = flag.Bool("oneshot", false, "Run one round of cleaning, honoring TTLs (ignores -path)")
	logLevel    = flag.String("log-level", "info", fmt.Sprintf("Log level is one of %v.", logrus.AllLevels))
	dryRun      = flag.Bool("dry-run", false, "If set, don't delete any resources, only log what would be done")
	ttlTagKey   = flag.String("ttl-tag-key", "", "If set, allow resources to use a tag with this key to override TTL")
	pushGateway = flag.String("push-gateway", "", "If specified, push prometheus metrics to this endpoint.")

	excludeTags common.CommaSeparatedStrings
	includeTags common.CommaSeparatedStrings

	sweepCount int

	cleaningTimeHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "aws_janitor_job_duration_time_seconds",
		ConstLabels: prometheus.Labels{},
		Buckets:     prometheus.ExponentialBuckets(1, 1.4, 30),
	}, []string{"type", "status", "region"})

	sweepCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "aws_janitor_swept_resources",
		ConstLabels: prometheus.Labels{},
	}, []string{"type", "status", "region"})
)

func init() {
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

	// If Prometheus PushGateway is configured, then before exit push the metric
	// to the PushGateway instance, otherwise just exit
	exitCode := 2
	startProcess := time.Now()
	if *pushGateway != "" {
		registry := prometheus.NewRegistry()
		registry.MustRegister(cleaningTimeHistogram, sweepCounter)
		pusher := push.New(*pushGateway, "aws-janitor").Gatherer(registry)

		defer func() {
			pushMetricBeforeExit(pusher, startProcess, exitCode)
			os.Exit(exitCode)
		}()
	} else {
		defer func() {
			os.Exit(exitCode)
		}()
	}

	// Retry aggressively (with default back-off). If the account is
	// in a really bad state, we may be contending with API rate
	// limiting and fighting against the very resources we're trying
	// to delete.
	sess := session.Must(session.NewSessionWithOptions(session.Options{Config: aws.Config{MaxRetries: aws.Int(100)}}))
	acct, err := account.GetAccount(sess, regions.Default)
	if err != nil {
		logrus.Errorf("Failed retrieving account: %v", err)
		runtime.Goexit()
	}
	logrus.Debugf("account: %s", acct)

	excludeTM, err := resources.TagMatcherForTags(excludeTags)
	if err != nil {
		logrus.Errorf("Error parsing --exclude-tags: %v", err)
		runtime.Goexit()
	}
	includeTM, err := resources.TagMatcherForTags(includeTags)
	if err != nil {
		logrus.Errorf("Error parsing --include-tags: %v", err)
		runtime.Goexit()
	}

	opts := resources.Options{
		Session:     sess,
		Account:     acct,
		DryRun:      *dryRun,
		ExcludeTags: excludeTM,
		IncludeTags: includeTM,
		TTLTagKey:   *ttlTagKey,
	}

	if *cleanAll {
		if err := resources.CleanAll(opts, *region); err != nil {
			logrus.Errorf("Error cleaning all resources: %v", err)
			runtime.Goexit()
		}
	} else if *oneShot {
		opts.DefaultTTL = *maxTTL
		if err := resources.CleanAll(opts, *region); err != nil {
			logrus.Fatalf("Error cleaning resources by TTL: %v", err)
		}
	} else if err := markAndSweep(opts, *region); err != nil {
		logrus.Errorf("Error marking and sweeping resources: %v", err)
		runtime.Goexit()
	}

	exitCode = 0
}

func markAndSweep(opts resources.Options, region string) error {
	s3p, err := s3path.GetPath(opts.Session, *path)
	if err != nil {
		return errors.Wrapf(err, "-path %q isn't a valid S3 path", *path)
	}

	regionList, err := regions.ParseRegion(opts.Session, region)
	if err != nil {
		return err
	}
	logrus.Infof("Regions: %+v", regionList)

	res, err := resources.LoadSet(opts.Session, s3p, *maxTTL)
	if err != nil {
		return errors.Wrapf(err, "Error loading %q", *path)
	}

	for _, region := range regionList {
		opts.Region = region
		for _, typ := range resources.RegionalTypeList {
			if err := typ.MarkAndSweep(opts, res); err != nil {
				return errors.Wrapf(err, "Error sweeping %T", typ)
			}
		}
	}

	opts.Region = regions.Default
	for _, typ := range resources.GlobalTypeList {
		if err := typ.MarkAndSweep(opts, res); err != nil {
			return errors.Wrapf(err, "Error sweeping %T", typ)
		}
	}

	sweepCount = res.MarkComplete()
	if err := res.Save(opts.Session, s3p); err != nil {
		return errors.Wrapf(err, "Error saving %q", *path)
	}

	logrus.Infof("swept %d resources", sweepCount)

	return nil
}

func pushMetricBeforeExit(pusher *push.Pusher, startTime time.Time, exitCode int) {
	// Set the status of the job
	status := "failed"
	if exitCode == 0 {
		status = "success"
	}

	// Set the type of the job to report the metric
	var job string
	if !*cleanAll {
		job = "mark_and_sweep"

		sweepCounter.
			With(prometheus.Labels{"type": job, "status": status, "region": *region}).
			Add(float64(sweepCount))
	} else {
		job = "clean_all"
	}

	duration := time.Since(startTime).Seconds()
	cleaningTimeHistogram.
		With(prometheus.Labels{"type": job, "status": status, "region": *region}).
		Observe(duration)

	if err := pusher.Add(); err != nil {
		logrus.Errorf("Could not push to Pushgateway: %v", err)
	}
}
