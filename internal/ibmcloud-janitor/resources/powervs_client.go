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
	"github.com/IBM-Cloud/power-go-client/ibmpisession"
	"github.com/IBM-Cloud/power-go-client/power/models"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"sigs.k8s.io/boskos/common"
	"sigs.k8s.io/boskos/common/ibmcloud"
	"sigs.k8s.io/boskos/internal/ibmcloud-janitor/account"
)

type IBMPowerVSClient struct {
	session *ibmpisession.IBMPISession

	instance  *PowerVSInstance
	network   *PowerVSNetwork
	workspace *PowerVSWorksapce
	resource  *common.Resource
}

// Returns the virtual server instances in the PowerVS service instance
func (p *IBMPowerVSClient) GetInstances() (*models.PVMInstances, error) {
	return p.instance.instanceClient.GetAll()
}

// Deletes the virtual server instances in the PowerVS service instance
func (p *IBMPowerVSClient) DeleteInstance(id string) error {
	return p.instance.instanceClient.Delete(id)
}

// Returns the networks in the PowerVS service instance
func (p *IBMPowerVSClient) GetNetworks() (*models.Networks, error) {
	return p.network.networkClient.GetAll()
}

// Deletes the network in PowerVS service instance
func (p *IBMPowerVSClient) DeleteNetwork(id string) error {
	return p.network.networkClient.Delete(id)
}

// Returns ports of the network instance
func (p *IBMPowerVSClient) GetPorts(id string) (*models.NetworkPorts, error) {
	return p.network.networkClient.GetAllPorts(id)
}

// Deletes the port of the network
func (p *IBMPowerVSClient) DeletePort(networkID, portID string) error {
	return p.network.networkClient.DeletePort(networkID, portID)
}

// Returns the details of the PowerVS workspace
func (p *IBMPowerVSClient) GetWorkspaceDetails() (*models.Workspace, error) {
	return p.workspace.workspaceClient.Get(p.workspace.serviceInstanceID)
}

// Returns a new PowerVS client
func NewPowerVSClient(options *CleanupOptions) (*IBMPowerVSClient, error) {
	var accountID *string
	resourceLogger := logrus.WithFields(logrus.Fields{"resource": options.Resource.Name})
	pclient := &IBMPowerVSClient{}
	powervsData, err := ibmcloud.GetPowerVSResourceData(options.Resource)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get the resource data")
	}

	auth, err := account.GetAuthenticator()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get the authenticator")
	}

	sclient, err := NewServiceIDClient(auth, &APIKey{serviceIDName: options.Resource.Name})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create serviceID client")
	}

	if options.AccountID != nil {
		accountID = options.AccountID
	} else {
		accountID, err = sclient.GetAccount()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get the account ID")
		}
		options.AccountID = accountID
	}

	clientOptions := &ibmpisession.IBMPIOptions{
		Debug:         options.Debug,
		Authenticator: auth,
		Zone:          powervsData.Zone,
		UserAccount:   *accountID,
	}
	pclient.session, err = ibmpisession.NewIBMPISession(clientOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create a new session")
	}
	resourceLogger.Info("successfully created PowerVS client")

	pclient.instance = NewInstanceClient(pclient.session, powervsData.ServiceInstanceID)
	pclient.network = NewNetworkClient(pclient.session, powervsData.ServiceInstanceID)
	pclient.workspace = NewWorkspaceClient(pclient.session, powervsData.ServiceInstanceID)
	pclient.resource = options.Resource

	return pclient, nil
}
