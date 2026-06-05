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
	"context"
	"strings"
	"time"

	"github.com/IBM/vpc-go-sdk/vpcv1"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	fipDeletionTimeout  = time.Minute
	fipPollingInterval  = time.Second * 10
	fipNotFoundPatterns = []string{"cannot be found", "not found"}
)

func deleteResourceGroupFloatingIPs(client *IBMVPCClient, resourceLogger *logrus.Entry) error {
	fips, _, err := client.ListFloatingIps(&vpcv1.ListFloatingIpsOptions{
		ResourceGroupID: &client.ResourceGroupID,
	})
	if err != nil {
		return errors.Wrap(err, "failed to list the floating IPs")
	}
	for _, fip := range fips.FloatingIps {
		if fip.ID == nil {
			continue
		}
		if err := deleteFloatingIP(client, *fip.ID, resourceLogger); err != nil {
			return err
		}
	}
	return nil
}

func deleteFloatingIP(client *IBMVPCClient, id string, resourceLogger *logrus.Entry) error {
	if err := triggerFloatingIPDeletion(client, id); err != nil {
		return err
	}
	if err := waitForFloatingIPDeleted(client, id); err != nil {
		return err
	}
	resourceLogger.WithField("id", id).Info("Successfully deleted the floating IP")
	return nil
}

func triggerFloatingIPDeletion(client *IBMVPCClient, id string) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(context.Background(), fipPollingInterval, fipDeletionTimeout, true, func(_ context.Context) (bool, error) {
		_, err := client.DeleteFloatingIP(&vpcv1.DeleteFloatingIPOptions{
			ID: &id,
		})
		if err == nil {
			return true, nil
		}
		if isFloatingIPNotFound(err) {
			return true, nil
		}
		lastErr = err
		return false, nil
	})
	if err != nil {
		if lastErr != nil {
			err = lastErr
		}
		return errors.Wrapf(err, "failed to delete the floating IP %q", id)
	}
	return nil
}

func waitForFloatingIPDeleted(client *IBMVPCClient, id string) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(context.Background(), fipPollingInterval, fipDeletionTimeout, true, func(_ context.Context) (bool, error) {
		_, _, err := client.GetFloatingIP(&vpcv1.GetFloatingIPOptions{ID: &id})
		if err == nil {
			return false, nil
		}
		if isFloatingIPNotFound(err) {
			return true, nil
		}
		lastErr = err
		return false, nil
	})
	if err != nil {
		if lastErr != nil {
			err = lastErr
		}
		return errors.Wrapf(err, "timed out waiting for floating IP %q to be deleted", id)
	}
	return nil
}

func isFloatingIPNotFound(err error) bool {
	for _, pattern := range fipNotFoundPatterns {
		if strings.Contains(err.Error(), pattern) {
			return true
		}
	}
	return false
}
