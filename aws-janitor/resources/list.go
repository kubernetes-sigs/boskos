/*
Copyright 2016 The Kubernetes Authors.

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
	aws2 "github.com/aws/aws-sdk-go-v2/aws"
)

// Options holds parameters for resource functions.
type Options struct {
	Config  *aws2.Config `json:"-"`
	Account string
	Region  string

	// Only resources which contain all IncludeTags will be considered for cleanup.
	IncludeTags TagMatcher
	// Any resources with at least one tag in ExcludeTags will be excluded from cleanup.
	// ExcludeTags takes precedence over IncludeTags - i.e. a resource that matches both
	// will be excluded.
	ExcludeTags TagMatcher

	// If set, any resources with a tag matching this key can override the global TTL (unless the global TTL is 0).
	// The value of the tag must be a valid Go time.Duration string.
	TTLTagKey string

	// Whether to actually delete resources, or just report what would be deleted.
	DryRun bool

	// If true, clean target groups.
	EnableTargetGroupClean bool

	// If true, clean key pairs.
	EnableKeyPairsClean bool

	// If true, clean VPC endpoints.
	EnableVPCEndpointsClean bool

	// If true, skip managed zone check and managed resource name check.
	SkipRoute53ManagementCheck bool

	// If true, clean DNS zones.
	EnableDNSZoneClean bool

	// If true, clean S3 Buckets.
	EnableS3BucketsClean bool

	// Resource record set types that shoud not be deleted.
	SkipResourceRecordSetTypes map[string]bool
}

type Type interface {
	// MarkAndSweep queries the resource in a specific region, using
	// the provided session (which has account-number acct), calling
	// res.Mark(<resource>) on each resource and deleting
	// appropriately.
	MarkAndSweep(opts Options, res *Set) error

	// ListAll queries all the resources this account has access to
	ListAll(opts Options) (*Set, error)
}

// AWS resource types known to this script, in dependency order.
var RegionalTypeList = []Type{
	CloudFormationStacks{},
	EKS{},
	ClassicLoadBalancers{},
	LoadBalancers{},
	AutoScalingGroups{},
	LaunchConfigurations{},
	LaunchTemplates{},
	Instances{},
	VPCEndpoints{},
	// Addresses
	NetworkInterfaces{},
	Subnets{},
	SecurityGroups{},
	// NetworkACLs
	// VPN Connections
	InternetGateways{},
	RouteTables{},
	NATGateway{},
	VPCs{},
	DHCPOptions{},
	Snapshots{},
	Volumes{},
	Addresses{},
	ElasticFileSystems{},
	SQSQueues{},
	// ELBV2 target groups,
	TargetGroups{},
	KeyPairs{},
	S3Bucket{},
	DedicatedHosts{},
}

// Non-regional AWS resource types, in dependency order
var GlobalTypeList = []Type{
	IAMInstanceProfiles{},
	IAMRoles{},
	IAMOIDCProviders{},
	Route53ResourceRecordSets{},
}
