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

// Clean up of network resources in done in the following order:
// 1. Unset and delete public gateways attached to a subnet
// 2. Delete the subnet
// 3. Delete floating IPs
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
	// List subnets with optional VPC filter
	if client.VPCID != "" {
		listSubnetOpts.VPCID = &client.VPCID
	}

	subnetList, _, err := client.ListSubnets(listSubnetOpts)
	if err != nil {
		return errors.Wrap(err, "failed to list the subnets")
	}

	for _, subnet := range subnetList.Subnets {
		pg, _, err := client.GetSubnetPublicGateway(&vpcv1.GetSubnetPublicGatewayOptions{
			ID: subnet.ID,
		})
		if pg != nil && err == nil {
			_, err := client.UnsetSubnetPublicGateway(&vpcv1.UnsetSubnetPublicGatewayOptions{
				ID: subnet.ID,
			})
			if err != nil {
				return errors.Wrapf(err, "failed to unset the gateway for %q", *subnet.Name)
			}

			_, err = client.DeletePublicGateway(&vpcv1.DeletePublicGatewayOptions{
				ID: pg.ID,
			})
			if err != nil {
				return errors.Wrapf(err, "failed to delete the gateway %q", *pg.Name)
			}
			resourceLogger.WithFields(logrus.Fields{"name": pg.Name}).Info("Successfully deleted the gateway")
		}
		_, err = client.DeleteSubnet(&vpcv1.DeleteSubnetOptions{ID: subnet.ID})
		if err != nil {
			return errors.Wrapf(err, "failed to delete the subnet %q", *subnet.Name)
		}
	}

	// Delete the unbound floating IPs that were previously used by a VSI
	fips, _, err := client.ListFloatingIps(&vpcv1.ListFloatingIpsOptions{
		ResourceGroupID: &client.ResourceGroupID,
	})
	if err != nil {
		return errors.Wrap(err, "failed to list the floating IPs")
	}
	for _, fip := range fips.FloatingIps {
		if client.VPCID != "" && fip.Target != nil {
			// Skip bound FIPs if VPC ID is specified
			continue
		}
		_, err = client.DeleteFloatingIP(&vpcv1.DeleteFloatingIPOptions{
			ID: fip.ID,
		})
		if err != nil {
			return errors.Wrapf(err, "failed to delete the floating IP %q", *fip.Name)
		}
		resourceLogger.WithFields(logrus.Fields{"name": fip.Name}).Info("Successfully deleted the floating IP")
	}

	resourceLogger.Info("Successfully deleted subnets and floating IPs")
	return nil
}
