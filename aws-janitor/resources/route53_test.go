/*
Copyright 2019 The Kubernetes Authors.

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
	"testing"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	route53v2types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

func TestManagedNames(t *testing.T) {
	grid := []struct {
		rrs      *route53v2types.ResourceRecordSet
		expected bool
	}{
		{
			rrs:      &route53v2types.ResourceRecordSet{Type: "A", Name: aws2.String("api.e2e-71149fffac-dba53.test-cncf-aws.k8s.io.")},
			expected: true,
		},
		{
			rrs:      &route53v2types.ResourceRecordSet{Type: "A", Name: aws2.String("api.internal.e2e-71149fffac-dba53.test-cncf-aws.k8s.io.")},
			expected: true,
		},
		{
			rrs:      &route53v2types.ResourceRecordSet{Type: "A", Name: aws2.String("main.etcd.e2e-71149fffac-dba53.test-cncf-aws.k8s.io.")},
			expected: true,
		},
		{
			rrs:      &route53v2types.ResourceRecordSet{Type: "A", Name: aws2.String("events.etcd.e2e-71149fffac-dba53.test-cncf-aws.k8s.io.")},
			expected: true,
		},
		{
			// Ignores non-A records
			rrs:      &route53v2types.ResourceRecordSet{Type: "CNAME", Name: aws2.String("api.e2e-71149fffac-dba53.test-cncf-aws.k8s.io.")},
			expected: false,
		},
		{
			// Must ignore the hosted zone system records
			rrs:      &route53v2types.ResourceRecordSet{Type: "NS", Name: aws2.String("test-cncf-aws.k8s.io.")},
			expected: false,
		},
		{
			// Must ignore the hosted zone system records
			rrs:      &route53v2types.ResourceRecordSet{Type: "SOA", Name: aws2.String("test-cncf-aws.k8s.io.")},
			expected: false,
		},
		{
			// Ignore names that are from tests that reuse cluster names
			rrs:      &route53v2types.ResourceRecordSet{Type: "A", Name: aws2.String("api.e2e-e2e-kops-aws.test-cncf-aws.k8s.io.")},
			expected: false,
		},
		{
			// Ignore arbitrary name
			rrs:      &route53v2types.ResourceRecordSet{Type: "A", Name: aws2.String("website.test-cncf-aws.k8s.io.")},
			expected: false,
		},
	}
	for _, g := range grid {
		actual := resourceRecordSetIsManaged(g.rrs)
		if actual != g.expected {
			t.Errorf("resource record %+v expected=%t actual=%t", g.rrs, g.expected, actual)
		}
	}
}
