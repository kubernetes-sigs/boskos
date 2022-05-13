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
	"strings"

	"github.com/IBM-Cloud/power-go-client/power/models"
	"github.com/IBM/go-sdk-core/v5/core"
	identityv1 "github.com/IBM/platform-services-go-sdk/iamidentityv1"
	"github.com/pkg/errors"
	"sigs.k8s.io/boskos/common"
)

type Resource interface {
	cleanup(options *CleanupOptions) error
}

type CleanupOptions struct {
	Resource *common.Resource
	Debug    bool
}

var PowervsResources = []Resource{
	PowervsInstance{},
	PowervsNetwork{},
}

var CommonResources = []Resource{
	APIKey{},
}

type PowerVS interface {
	GetNetworks() (*models.Networks, error)
	DeleteNetwork(id string) error
	GetInstances() (*models.PVMInstances, error)
	DeleteInstance(id string) error
	GetPorts(id string) (*models.NetworkPorts, error)
	DeletePort(networkID, portID string) error
}

type ServiceIDClient interface {
	CreateAPIKey(options *identityv1.CreateAPIKeyOptions) (*identityv1.APIKey, *core.DetailedResponse, error)
	DeleteAPIKey(options *identityv1.DeleteAPIKeyOptions) (*core.DetailedResponse, error)
	ListAPIKeys(options *identityv1.ListAPIKeysOptions) (*identityv1.APIKeyList, *core.DetailedResponse, error)
	GetAPIKeysDetails(*identityv1.GetAPIKeysDetailsOptions) (*identityv1.APIKey, *core.DetailedResponse, error)
	ListServiceID(options *identityv1.ListServiceIdsOptions) (*identityv1.ServiceIDList, *core.DetailedResponse, error)
}

func listResources(rtype string) ([]Resource, error) {
	if strings.HasPrefix(rtype, "powervs") {
		return PowervsResources, nil
	}
	return nil, errors.New("Not a valid resource type. Only supported type is powervs-service")
}
