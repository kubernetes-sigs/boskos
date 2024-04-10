/*
Copyright 2017 The Kubernetes Authors.

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
	"time"

	"github.com/sirupsen/logrus"

	"k8s.io/test-infra/prow/logrusutil"
	"sigs.k8s.io/boskos/client"
	"sigs.k8s.io/boskos/common"
)

var (
	rTypes            common.CommaSeparatedStrings
	rTypesConfig      string
	extraSourceStates common.CommaSeparatedStrings
	boskosURL         = flag.String("boskos-url", "http://boskos", "Boskos URL")
	username          = flag.String("username", "", "Username used to access the Boskos server")
	passwordFile      = flag.String("password-file", "", "The path to password file used to access the Boskos server")
	expiryDuration    = flag.Duration("expire", 30*time.Minute, "The expiry time (in minutes) after which reaper will reset resources.")
	targetState       = flag.String("target-state", common.Dirty, "The state to move resources to when reaped.")
)

type resetClient interface {
	Reset(string, string, time.Duration, string) (map[string]string, error)
}

func init() {
	flag.Var(&rTypes, "resource-type", "comma-separated list of resources need to be reset")
	flag.StringVar(&rTypesConfig, "resource-type-from-config", "", "extract a list of resources need to be reset from boskos' config file")
	flag.Var(&extraSourceStates, "extra-source-states", "comma-separated list of extra source states need to be reset")
}

func main() {
	logrusutil.ComponentInit()

	flag.Parse()
	boskos, err := client.NewClient("Reaper", *boskosURL, *username, *passwordFile)
	if err != nil {
		logrus.WithError(err).Fatal("unable to create a Boskos client")
	}
	logrus.Infof("Initialized boskos client!")

	if len(rTypes) == 0 && rTypesConfig == "" {
		logrus.Fatal("--resource-type or --resource-type-from-config must be set")
	}

	if targetState == nil {
		logrus.Fatal("--target-state must not be empty!")
	}

	var rt common.ResourceTypes
	if rt, err = common.NewResourceTypes(rTypes, rTypesConfig); err != nil {
		logrus.WithError(err).Fatal("new resource types")
	}
	logrus.Info("initialized resource types")

	reap := &reaper{extraSourceStates, *expiryDuration, *targetState}

	logrus.Infof("resource types: %+v", rt.Types())
	for range time.Tick(time.Minute) {
		for _, r := range rt.Types() {
			reap.sync(boskos, r)
		}
	}
}

type reaper struct {
	extraSourceStates common.CommaSeparatedStrings
	expiryDuration    time.Duration
	targetState       string
}

func (r *reaper) sync(c resetClient, res string) {
	log := logrus.WithField("resource_type", res).WithField("target_state", r.targetState)

	// kubetest busted
	log = log.WithField("source_state", common.Busy)
	if owners, err := c.Reset(res, common.Busy, r.expiryDuration, r.targetState); err != nil {
		log.WithError(err).Error("Reset failed")
	} else {
		logResponses(log, owners)
	}

	// janitor, mason busted
	log = log.WithField("source_state", common.Cleaning)
	if owners, err := c.Reset(res, common.Cleaning, r.expiryDuration, r.targetState); err != nil {
		log.WithError(err).Error("Reset failed")
	} else {
		logResponses(log, owners)
	}

	// mason busted
	log = log.WithField("source_state", common.Leased)
	if owners, err := c.Reset(res, common.Leased, r.expiryDuration, r.targetState); err != nil {
		log.WithError(err).Error("Reset failed")
	} else {
		logResponses(log, owners)
	}

	// extra source states
	for _, s := range r.extraSourceStates {
		log = log.WithField("source_state", s)
		if owners, err := c.Reset(res, s, r.expiryDuration, r.targetState); err != nil {
			log.WithError(err).Error("Reset failed")
		} else {
			logResponses(log, owners)
		}
	}
}

func logResponses(parent *logrus.Entry, response map[string]string) {
	for name, previousOwner := range response {
		parent.WithField("resource_name", name).WithField("previous_owner", previousOwner).Info("Reset resource")
	}
}
