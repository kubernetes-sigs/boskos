/*
Copyright 2026 The Kubernetes Authors.

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
	"net/http"

	"github.com/IBM/vpc-go-sdk/vpcv1"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func deleteSubnetPublicGateway(client *IBMVPCClient, subnet vpcv1.Subnet, resourceLogger *logrus.Entry) error {
	pg, response, err := client.GetSubnetPublicGateway(&vpcv1.GetSubnetPublicGatewayOptions{
		ID: subnet.ID,
	})
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return nil
		}
		return errors.Wrap(err, "failed to get the subnet public gateway")
	}
	if pg == nil {
		return nil
	}
	return deletePublicGatewayAndFloatingIP(client, *pg, subnet.ID, resourceLogger)
}

func deleteTargetVPCPublicGateways(client *IBMVPCClient, resourceLogger *logrus.Entry) error {
	publicGateways, _, err := client.ListPublicGateways(&vpcv1.ListPublicGatewaysOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to list the public gateways")
	}

	for _, pg := range publicGateways.PublicGateways {
		if pg.VPC == nil || pg.VPC.ID == nil || *pg.VPC.ID != client.VPCID {
			continue
		}
		if err := deletePublicGatewayAndFloatingIP(client, pg, nil, resourceLogger); err != nil {
			return err
		}
	}

	return nil
}

func deletePublicGatewayAndFloatingIP(client *IBMVPCClient, pg vpcv1.PublicGateway, subnetID *string, resourceLogger *logrus.Entry) error {
	floatingIPID := ""
	if pg.FloatingIP != nil && pg.FloatingIP.ID != nil {
		floatingIPID = *pg.FloatingIP.ID
	}
	if subnetID != nil {
		_, err := client.UnsetSubnetPublicGateway(&vpcv1.UnsetSubnetPublicGatewayOptions{
			ID: subnetID,
		})
		if err != nil {
			return errors.Wrap(err, "failed to unset the subnet public gateway")
		}
	}

	_, err := client.DeletePublicGateway(&vpcv1.DeletePublicGatewayOptions{
		ID: pg.ID,
	})
	if err != nil {
		return errors.Wrapf(err, "failed to delete the gateway %q", *pg.Name)
	}
	resourceLogger.WithFields(logrus.Fields{"name": *pg.Name}).Info("Successfully deleted the gateway")
	if floatingIPID != "" {
		if err := deleteFloatingIP(client, floatingIPID, resourceLogger); err != nil {
			return err
		}
	}
	return nil
}
