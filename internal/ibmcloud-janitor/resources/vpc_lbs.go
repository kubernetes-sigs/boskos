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
	"time"

	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
)

type VPCLoadBalancer struct{}

var (
	lbDeletionTimeout = time.Minute * 4
	lbPollingInterval = time.Second * 30
	resourceLogger    *logrus.Entry
)

// Cleans up the load balancers in a given region
func (VPCLoadBalancer) cleanup(options *CleanupOptions) error {
	resourceLogger = logrus.WithFields(logrus.Fields{"resource": options.Resource.Name})
	resourceLogger.Info("Cleaning up the load balancers")
	client, err := NewVPCClient(options)
	if err != nil {
		return errors.Wrap(err, "couldn't create VPC client")
	}

	var deletedLBList []string
	var errs []error

	f := func(start string) (bool, string, error) {
		listLbOpts := &vpcv1.ListLoadBalancersOptions{}
		if start != "" {
			listLbOpts.Start = &start
		}

		loadBalancers, _, err := client.ListLoadBalancers(listLbOpts)
		if err != nil {
			return false, "", errors.Wrap(err, "failed to list the load balancers")
		}

		if len(loadBalancers.LoadBalancers) == 0 {
			resourceLogger.Info("there are no available load balancers to delete")
			return true, "", nil
		}

		for _, lb := range loadBalancers.LoadBalancers {
			if shouldSkipLoadBalancer(lb, client, &errs) {
				continue
			}

			if _, err := client.DeleteLoadBalancer(&vpcv1.DeleteLoadBalancerOptions{
				ID: lb.ID,
			}); err != nil {
				resourceLogger.WithField("name", *lb.Name).Error("failed to delete load balancer")
				errs = append(errs, err)
				continue
			}
			deletedLBList = append(deletedLBList, *lb.ID)
			resourceLogger.WithField("name", *lb.Name).Info("load balancer deletion triggered")
		}

		if loadBalancers.Next != nil && *loadBalancers.Next.Href != "" {
			return false, *loadBalancers.Next.Href, nil
		}

		return true, "", nil
	}

	if err = pagingHelper(f); err != nil {
		errs = append(errs, errors.Wrapf(err, "failed to delete the load balancers"))
	}

	// check if the LBs are properly deleted
	checkLBs(deletedLBList, client, &errs)

	if len(errs) > 0 {
		return kerrors.NewAggregate(errs)
	}

	resourceLogger.Info("Successfully deleted the load balancers")
	return nil
}

func shouldSkipLoadBalancer(lb vpcv1.LoadBalancer, client *IBMVPCClient, errs *[]error) bool {
	if client.VPCID == "" {
		// No VPC ID is provided, check if LB's resource group matches the ID passed in user-data
		return lb.ResourceGroup == nil || *lb.ResourceGroup.ID != client.ResourceGroupID
	}

	// Check if subnets belong to the VPC using ID passed in user-data. If yes, skip deletion of LB
	for _, subnetRef := range lb.Subnets {
		subnet, _, err := client.GetSubnet(&vpcv1.GetSubnetOptions{ID: subnetRef.ID})
		if err != nil {
			resourceLogger.WithFields(logrus.Fields{
				"name":      *lb.Name,
				"subnet_id": subnetRef.ID,
			}).Error("failed to get subnet details")
			*errs = append(*errs, err)
			continue
		}
		if subnet.VPC != nil && *subnet.VPC.ID == client.VPCID {
			return true
		}
	}
	resourceLogger.WithField("name", *lb.Name).Warn("load balancer has no attached subnets, assuming stale and proceeding with deletion")
	return false
}

func checkLBs(list []string, client *IBMVPCClient, errs *[]error) {
	check := func(id string) error {
		return wait.PollImmediate(lbPollingInterval, lbDeletionTimeout, func() (bool, error) {
			_, _, err := client.GetLoadBalancer(&vpcv1.GetLoadBalancerOptions{ID: &id})
			if err != nil && strings.Contains(err.Error(), "cannot be found") {
				return true, nil
			}
			return false, err
		})
	}

	for _, lb := range list {
		if err := check(lb); err != nil {
			resourceLogger.WithField("ID", lb).Error("failed to check the deletion of load balancer")
			*errs = append(*errs, err)
		}
	}
}
