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
	"strings"
	"time"

	"github.com/IBM/vpc-go-sdk/vpcv1"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
)

type VPCInstance struct{}

var (
	instanceDeletionTimeout  = time.Minute * 5
	instancePollingInterval  = time.Second * 15
	instanceNotFoundPatterns = []string{"cannot be found", "not found"}
)

// Cleans up the virtual server instances in a given region
func (VPCInstance) cleanup(options *CleanupOptions) error {
	resourceLogger := logrus.WithFields(logrus.Fields{"resource": options.Resource.Name})
	resourceLogger.Info("Cleaning up the virtual server instances")
	client, err := NewVPCClient(options)
	if err != nil {
		return errors.Wrap(err, "couldn't create VPC client")
	}

	listInstanceOpts := &vpcv1.ListInstancesOptions{
		ResourceGroupID: &client.ResourceGroupID,
	}
	if client.VPCID != "" {
		listInstanceOpts.VPCID = &client.VPCID
	}

	instanceList, _, err := client.ListInstances(listInstanceOpts)
	if err != nil {
		return errors.Wrap(err, "failed to list the instances")
	}

	for _, ins := range instanceList.Instances {
		if ins.ID == nil {
			continue
		}
		instanceID := *ins.ID
		instanceName := ""
		if ins.Name != nil {
			instanceName = *ins.Name
		}

		if client.VPCID != "" {
			// Delete bound instance FIPs while their target VPC is still known.
			// Resource-group cleanup removes FIPs in VPCNetwork after instance deletion.
			if err := deleteInstanceFloatingIPs(client, instanceID, instanceName, resourceLogger); err != nil {
				return err
			}
		}
		_, err := client.DeleteInstance(&vpcv1.DeleteInstanceOptions{
			ID: &instanceID,
		})
		if err != nil {
			return errors.Wrapf(err, "failed to delete the instance %q", instanceName)
		}
		resourceLogger.WithField("name", instanceName).Info("instance deletion triggered")
		if err := waitForInstanceDeleted(client, instanceID, instanceName); err != nil {
			return err
		}
		resourceLogger.WithField("name", instanceName).Info("instance deletion completed")
	}
	resourceLogger.Info("Successfully deleted the virtual server instances")
	return nil
}

func deleteInstanceFloatingIPs(client *IBMVPCClient, instanceID, instanceName string, resourceLogger *logrus.Entry) error {
	interfaces, _, err := client.ListInstanceNetworkInterfaces(&vpcv1.ListInstanceNetworkInterfacesOptions{
		InstanceID: &instanceID,
	})
	if err != nil {
		return errors.Wrapf(err, "failed to list network interfaces for instance %q", instanceName)
	}

	for _, networkInterface := range interfaces.NetworkInterfaces {
		for _, fip := range networkInterface.FloatingIps {
			if fip.ID == nil {
				continue
			}
			if err := deleteFloatingIP(client, *fip.ID, resourceLogger); err != nil {
				return err
			}
		}
	}

	return nil
}

func waitForInstanceDeleted(client *IBMVPCClient, id, name string) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(context.Background(), instancePollingInterval, instanceDeletionTimeout, true, func(_ context.Context) (bool, error) {
		_, _, err := client.GetInstance(&vpcv1.GetInstanceOptions{ID: &id})
		if err == nil {
			return false, nil
		}
		if isInstanceNotFound(err) {
			return true, nil
		}
		lastErr = err
		return false, nil
	})
	if err != nil {
		if lastErr != nil {
			err = lastErr
		}
		return errors.Wrapf(err, "timed out waiting for instance %q to be deleted", name)
	}
	return nil
}

func isInstanceNotFound(err error) bool {
	for _, pattern := range instanceNotFoundPatterns {
		if strings.Contains(err.Error(), pattern) {
			return true
		}
	}
	return false
}
