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
	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"sigs.k8s.io/boskos/common"
	"sigs.k8s.io/boskos/common/ibmcloud"
	"sigs.k8s.io/boskos/internal/ibmcloud-janitor/account"
)

type IBMVPCClient struct {
	vpcService      *vpcv1.VpcV1
	ResourceGroupID string
	VPCID           string
	Resource        *common.Resource
}

func (c *IBMVPCClient) DeleteInstance(options *vpcv1.DeleteInstanceOptions) (*core.DetailedResponse, error) {
	return c.vpcService.DeleteInstance(options)
}

func (c *IBMVPCClient) ListInstances(options *vpcv1.ListInstancesOptions) (*vpcv1.InstanceCollection, *core.DetailedResponse, error) {
	return c.vpcService.ListInstances(options)
}

func (c *IBMVPCClient) DeleteVPC(options *vpcv1.DeleteVPCOptions) (*core.DetailedResponse, error) {
	return c.vpcService.DeleteVPC(options)
}

func (c *IBMVPCClient) ListVpcs(options *vpcv1.ListVpcsOptions) (*vpcv1.VPCCollection, *core.DetailedResponse, error) {
	return c.vpcService.ListVpcs(options)
}

func (c *IBMVPCClient) DeleteFloatingIP(options *vpcv1.DeleteFloatingIPOptions) (*core.DetailedResponse, error) {
	return c.vpcService.DeleteFloatingIP(options)
}

func (c *IBMVPCClient) ListFloatingIps(options *vpcv1.ListFloatingIpsOptions) (*vpcv1.FloatingIPCollection, *core.DetailedResponse, error) {
	return c.vpcService.ListFloatingIps(options)
}

func (c *IBMVPCClient) DeleteSubnet(options *vpcv1.DeleteSubnetOptions) (*core.DetailedResponse, error) {
	return c.vpcService.DeleteSubnet(options)
}

func (c *IBMVPCClient) ListSubnets(options *vpcv1.ListSubnetsOptions) (*vpcv1.SubnetCollection, *core.DetailedResponse, error) {
	return c.vpcService.ListSubnets(options)
}

func (c *IBMVPCClient) GetSubnetPublicGateway(options *vpcv1.GetSubnetPublicGatewayOptions) (*vpcv1.PublicGateway, *core.DetailedResponse, error) {
	return c.vpcService.GetSubnetPublicGateway(options)
}

func (c *IBMVPCClient) DeletePublicGateway(options *vpcv1.DeletePublicGatewayOptions) (*core.DetailedResponse, error) {
	return c.vpcService.DeletePublicGateway(options)
}

func (c *IBMVPCClient) UnsetSubnetPublicGateway(options *vpcv1.UnsetSubnetPublicGatewayOptions) (*core.DetailedResponse, error) {
	return c.vpcService.UnsetSubnetPublicGateway(options)
}

func (c *IBMVPCClient) DeleteLoadBalancer(options *vpcv1.DeleteLoadBalancerOptions) (*core.DetailedResponse, error) {
	return c.vpcService.DeleteLoadBalancer(options)
}

func (c *IBMVPCClient) ListLoadBalancers(options *vpcv1.ListLoadBalancersOptions) (*vpcv1.LoadBalancerCollection, *core.DetailedResponse, error) {
	return c.vpcService.ListLoadBalancers(options)
}

func (c *IBMVPCClient) GetLoadBalancer(options *vpcv1.GetLoadBalancerOptions) (result *vpcv1.LoadBalancer, response *core.DetailedResponse, err error) {
	return c.vpcService.GetLoadBalancer(options)
}

func (c *IBMVPCClient) GetSubnet(options *vpcv1.GetSubnetOptions) (*vpcv1.Subnet, *core.DetailedResponse, error) {
	return c.vpcService.GetSubnet(options)
}

// Creates a new VPC Client
func NewVPCClient(options *CleanupOptions) (*IBMVPCClient, error) {
	client := &IBMVPCClient{}
	resourceLogger := logrus.WithFields(logrus.Fields{"resource": options.Resource.Name})
	vpcData, err := ibmcloud.GetVPCResourceData(options.Resource)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get the resource data")
	}

	client.ResourceGroupID = vpcData.ResourceGroup
	client.VPCID = vpcData.VPCID
	client.Resource = options.Resource
	url := "https://" + vpcData.Region + ".iaas.cloud.ibm.com/v1"
	auth, err := account.GetAuthenticator()
	if err != nil {
		return nil, err
	}

	client.vpcService, err = vpcv1.NewVpcV1(&vpcv1.VpcV1Options{
		Authenticator: auth,
		URL:           url,
	})
	resourceLogger.Info("successfully created VPC client")

	if options.Debug {
		core.SetLoggingLevel(core.LevelDebug)
	}

	return client, err
}
