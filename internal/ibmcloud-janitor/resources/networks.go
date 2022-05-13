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
	"context"

	"github.com/IBM-Cloud/power-go-client/clients/instance"
	"github.com/IBM-Cloud/power-go-client/ibmpisession"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type PowervsNetwork struct {
	networkClient     *instance.IBMPINetworkClient
	serviceInstanceID string
}

// Creates a new PowerVS Network client
func NewNetworkClient(sess *ibmpisession.IBMPISession, instanceID string) *PowervsNetwork {
	c := &PowervsNetwork{
		serviceInstanceID: instanceID,
	}
	c.networkClient = instance.NewIBMPINetworkClient(context.Background(), sess, instanceID)
	return c
}

// Cleans up the networks in the PowerVS service instance
func (n PowervsNetwork) cleanup(options *CleanupOptions) error {
	resourceLogger := logrus.WithFields(logrus.Fields{"resource": options.Resource.Name})
	resourceLogger.Info("Cleaning up the networks")
	pclient, err := NewPowerVSClient(options)
	if err != nil {
		return errors.Wrap(err, "couldn't create powervs client")
	}

	networks, err := pclient.GetNetworks()
	if err != nil {
		return errors.Wrapf(err, "failed to get the networks in %q", pclient.resource.Name)
	}

	for _, net := range networks.Networks {
		ports, err := pclient.GetPorts(*net.NetworkID)
		if err != nil {
			return errors.Wrapf(err, "failed to get ports of network %q", *net.Name)
		}
		for _, port := range ports.Ports {
			err = pclient.DeletePort(*net.NetworkID, *port.PortID)
			if err != nil {
				return errors.Wrapf(err, "failed to delete port of network %q", *net.Name)
			}
		}
		err = pclient.DeleteNetwork(*net.NetworkID)
		if err != nil {
			return errors.Wrapf(err, "failed to delete network %q", *net.Name)
		}
	}
	resourceLogger.Info("Successfully deleted the networks")
	return nil
}
