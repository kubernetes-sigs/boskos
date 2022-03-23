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

package resources

import (
	"fmt"

	identityv1 "github.com/IBM/platform-services-go-sdk/iamidentityv1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/boskos/common/ibmcloud"
	"sigs.k8s.io/boskos/internal/ibmcloud-janitor/account"
)

type APIKey struct {
	serviceIDName string
	value         *string
}

func getAPIkeyName() string {
	n := rand.String(4)
	return fmt.Sprintf("test-key-%s", n)
}

// Lists and returns the target service ID
func (s *ServiceID) fetchServiceID(account *string) (*identityv1.ServiceID, error) {
	options := &identityv1.ListServiceIdsOptions{
		AccountID: account,
		Name:      &s.key.serviceIDName,
	}

	list, _, err := s.ListServiceID(options)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the serviceIDs")
	}

	if len(list.Serviceids) == 0 {
		return nil, errors.New("no service ID is present with the given name")
	} else if len(list.Serviceids) == 1 {
		return &list.Serviceids[0], nil
	} else {
		return nil, errors.New("failed to find the target service ID ")
	}
}

// Fetches the API keys under the target service ID and deletes them
// followed by creating a new API key
func (s *ServiceID) resetKeys(serviceID *identityv1.ServiceID) (*identityv1.APIKey, error) {
	options := &identityv1.ListAPIKeysOptions{
		AccountID: serviceID.AccountID,
		IamID:     serviceID.IamID,
	}

	apikeys, _, err := s.ListAPIKeys(options)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the API keys")
	}

	for i := 0; i < len(apikeys.Apikeys); i++ {
		deleteOptions := &identityv1.DeleteAPIKeyOptions{
			ID: apikeys.Apikeys[i].ID,
		}
		_, err = s.DeleteAPIKey(deleteOptions)
		if err != nil {
			return nil, errors.Wrap(err, "failed to delete API key")
		}
	}

	apikeyName := getAPIkeyName()
	apiKeyOptions := &identityv1.CreateAPIKeyOptions{
		Name:  &apikeyName,
		IamID: serviceID.IamID,
	}
	apiKey, _, err := s.CreateAPIKey(apiKeyOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create API key")
	}
	return apiKey, nil
}

func (k APIKey) cleanup(options *CleanupOptions) error {
	resourceLogger := logrus.WithFields(logrus.Fields{"resource": options.Resource.Name})
	resourceLogger.Info("Cleaning up the API key of resource")
	pclient, err := NewPowerVSClient(options)
	if err != nil {
		return errors.Wrap(err, "couldn't create powervs client")
	}

	auth, err := account.GetAuthenticator()
	if err != nil {
		return errors.Wrap(err, "failed to get authenticator")
	}

	sclient, err := NewServiceIDClient(auth, pclient.key)
	if err != nil {
		return errors.Wrap(err, "failed to create serviceID client")
	}

	account, err := sclient.GetAccount()
	if err != nil {
		return errors.Wrap(err, "failed to get the account ID")
	}

	serviceID, err := sclient.fetchServiceID(account)
	if err != nil {
		return errors.Wrap(err, "failed to fetch Service ID")
	}
	resourceLogger.WithField("Service ID: ", serviceID.Name).Info("found the target service ID")

	apikey, err := sclient.resetKeys(serviceID)
	if err != nil {
		return errors.Wrap(err, "failed to reset the API key")
	}
	resourceLogger.WithField("API key:", apikey.Name).Info("Successfully reset the API key of the resource")

	ibmcloud.UpdateResource(pclient.resource, *apikey.Apikey)
	return nil
}
