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

type PowervsInstance struct {
	instanceClient    *instance.IBMPIInstanceClient
	serviceInstanceID string
}

// Creates a new PowerVS Instance client
func NewInstanceClient(sess *ibmpisession.IBMPISession, instanceID string) *PowervsInstance {
	c := &PowervsInstance{
		serviceInstanceID: instanceID,
	}
	c.instanceClient = instance.NewIBMPIInstanceClient(context.Background(), sess, instanceID)
	return c
}

// Cleans up the virtual server instances in the PowerVS service instance
func (i PowervsInstance) cleanup(options *CleanupOptions) error {
	resourceLogger := logrus.WithFields(logrus.Fields{"resource": options.Resource.Name})
	resourceLogger.Info("Cleaning up the virtual server instances")
	pclient, err := NewPowerVSClient(options)
	if err != nil {
		return errors.Wrap(err, "couldn't create powervs client")
	}

	instances, err := pclient.GetInstances()
	if err != nil {
		return errors.Wrapf(err, "failed to get the instances in %q", pclient.resource.Name)
	}

	for _, ins := range instances.PvmInstances {
		err = pclient.DeleteInstance(*ins.PvmInstanceID)
		if err != nil {
			return errors.Wrapf(err, "failed to delete the instance %q", *ins.ServerName)
		}
	}
	resourceLogger.Info("Successfully deleted virtual server instances")
	return nil
}
