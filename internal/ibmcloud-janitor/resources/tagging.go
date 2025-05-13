/*
Copyright 2025 The Kubernetes Authors.

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
	"github.com/IBM/platform-services-go-sdk/globaltaggingv1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/boskos/internal/ibmcloud-janitor/account"
)

func NewTaggingClient() (*globaltaggingv1.GlobalTaggingV1, error) {
	auth, err := account.GetAuthenticator()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get the authenticator")
	}

	client, err := globaltaggingv1.NewGlobalTaggingV1(&globaltaggingv1.GlobalTaggingV1Options{
		Authenticator: auth,
	})
	if err != nil {
		return nil, err
	}

	logrus.Info("Successfully created tagging client")
	return client, nil
}
