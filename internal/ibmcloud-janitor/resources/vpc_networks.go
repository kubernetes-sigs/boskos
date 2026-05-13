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
	"github.com/IBM/vpc-go-sdk/vpcv1"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type VPCNetwork struct{}

// Clean up of network resources is done in the following order:
// 1. Unset and delete public gateways and their floating IPs attached to a subnet
// 2. Delete the subnet
// 3. Delete any remaining public gateways in the target VPC
func (VPCNetwork) cleanup(options *CleanupOptions) error {
	resourceLogger := logrus.WithFields(logrus.Fields{"resource": options.Resource.Name})
	resourceLogger.Info("Cleaning up the networks")
	client, err := NewVPCClient(options)
	if err != nil {
		return errors.Wrap(err, "couldn't create VPC client")
	}

	listSubnetOpts := &vpcv1.ListSubnetsOptions{
		ResourceGroupID: &client.ResourceGroupID,
	}
	if client.VPCID != "" {
		listSubnetOpts.VPCID = &client.VPCID
	}

	subnetList, _, err := client.ListSubnets(listSubnetOpts)
	if err != nil {
		return errors.Wrap(err, "failed to list the subnets")
	}

	for _, subnet := range subnetList.Subnets {
		if err := deleteSubnetPublicGateway(client, subnet, resourceLogger); err != nil {
			return err
		}
		_, err = client.DeleteSubnet(&vpcv1.DeleteSubnetOptions{ID: subnet.ID})
		if err != nil {
			return errors.Wrapf(err, "failed to delete the subnet %q", *subnet.Name)
		}
	}

	if client.VPCID != "" {
		// Delete orphan gateways left by partial Terraform setup or cleanup.
		if err := deleteTargetVPCPublicGateways(client, resourceLogger); err != nil {
			return err
		}
	} else if err := deleteResourceGroupFloatingIPs(client, resourceLogger); err != nil {
		return err
	}

	resourceLogger.Info("Successfully deleted VPC network resources")
	return nil
}
