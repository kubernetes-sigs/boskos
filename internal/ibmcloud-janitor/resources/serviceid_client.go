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
	"github.com/IBM/go-sdk-core/v5/core"
	identityv1 "github.com/IBM/platform-services-go-sdk/iamidentityv1"
	"github.com/pkg/errors"
)

type ServiceID struct {
	key            *APIKey
	identityClient *identityv1.IamIdentityV1
}

// Returns list of service IDs
func (s *ServiceID) ListServiceID(options *identityv1.ListServiceIdsOptions) (*identityv1.ServiceIDList, *core.DetailedResponse, error) {
	return s.identityClient.ListServiceIds(options)
}

// Creates a new API Key
func (s *ServiceID) CreateAPIKey(options *identityv1.CreateAPIKeyOptions) (*identityv1.APIKey, *core.DetailedResponse, error) {
	return s.identityClient.CreateAPIKey(options)
}

// Deletes the API Key
func (s *ServiceID) DeleteAPIKey(options *identityv1.DeleteAPIKeyOptions) (*core.DetailedResponse, error) {
	return s.identityClient.DeleteAPIKey(options)
}

// Lists the API keys of a service ID
func (s *ServiceID) ListAPIKeys(options *identityv1.ListAPIKeysOptions) (*identityv1.APIKeyList, *core.DetailedResponse, error) {
	return s.identityClient.ListAPIKeys(options)
}

// Returns the details of a given API key
func (s *ServiceID) GetAPIKeysDetails(options *identityv1.GetAPIKeysDetailsOptions) (*identityv1.APIKey, *core.DetailedResponse, error) {
	return s.identityClient.GetAPIKeysDetails(options)
}

// Returns a new Service ID client
func NewServiceIDClient(auth core.Authenticator, key *APIKey) (*ServiceID, error) {
	identityv1Options := &identityv1.IamIdentityV1Options{
		Authenticator: auth,
	}
	identityClient, err := identityv1.NewIamIdentityV1(identityv1Options)
	if err != nil {
		return nil, err
	}
	return &ServiceID{
		key:            key,
		identityClient: identityClient,
	}, nil
}

// Returns the account ID
func (s *ServiceID) GetAccount() (*string, error) {
	apikeyDetailsOptions := &identityv1.GetAPIKeysDetailsOptions{
		IamAPIKey: s.key.value,
	}

	apiKeyDetails, _, err := s.GetAPIKeysDetails(apikeyDetailsOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get details for the APIKey")
	}
	return apiKeyDetails.AccountID, nil
}
