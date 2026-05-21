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

package client

import (
	"fmt"
	"net/url"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/boskos/simpleclient"
	"sigs.k8s.io/prow/pkg/config/secret"
)

var (
	// ErrNotFound is returned by Acquire() when no resources are available.
	ErrNotFound = simpleclient.ErrNotFound
	// ErrAlreadyInUse is returned by Acquire when resources are already being requested.
	ErrAlreadyInUse = simpleclient.ErrAlreadyInUse
	// ErrContextRequired is returned by AcquireWait and AcquireByStateWait when
	// they are invoked with a nil context.
	ErrContextRequired = simpleclient.ErrContextRequired
	// ErrTypeNotFound is returned when the requested resource type (rtype) does not exist.
	// For this error to be returned, you must set DistinguishNotFoundVsTypeNotFound to true.
	ErrTypeNotFound = simpleclient.ErrTypeNotFound
)

// Client defines the public Boskos client object
type Client = simpleclient.Client

// NewClient creates a Boskos client for the specified URL and resource owner.
//
// Clients created with this function default to retrying failed connection
// attempts three times with a ten second pause between each attempt.
func NewClient(owner string, urlString, username, passwordFile string) (*Client, error) {

	if (username == "") != (passwordFile == "") {
		return nil, fmt.Errorf("username and passwordFile must be specified together")
	}

	var getPassword func() []byte
	if passwordFile != "" {
		u, err := url.Parse(urlString)
		if err != nil {
			return nil, err
		}
		if u.Scheme != "https" {
			// returning error here would make the tests hard
			// we print out a warning message here instead
			fmt.Printf("[WARNING] should NOT use password without enabling TLS: '%s'\n", urlString)
		}

		if err := secret.Add(passwordFile); err != nil {
			logrus.WithError(err).Fatal("Failed to start secrets agent")
		}
		getPassword = secret.GetTokenGenerator(passwordFile)
	}

	return simpleclient.NewClient(owner, urlString, username, getPassword)
}

// NewClient creates a Boskos client for the specified URL and resource owner.
//
// Clients created with this function default to retrying failed connection
// attempts three times with a ten second pause between each attempt.
func NewClientWithPasswordGetter(owner string, urlString, username string, passwordGetter func() []byte) (*Client, error) {
	return simpleclient.NewClient(owner, urlString, username, passwordGetter)
}

// SleepFunc is called when requests are retried. May be replaced in tests with *SleepFunc = <new function>.
var SleepFunc = &simpleclient.SleepFunc
