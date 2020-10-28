/*
Copyright 2020 The Kubernetes Authors.

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

	"github.com/sirupsen/logrus"

	"sigs.k8s.io/boskos/common"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to init resource file")
	flag.Parse()

	config, err := common.ParseConfig(*configPath)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to parse config")
	}

	if err := common.ValidateConfig(config); err != nil {
		logrus.WithError(err).Fatal("Config validation failed")
	}
}
