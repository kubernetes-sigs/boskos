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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	configv2 "github.com/aws/aws-sdk-go-v2/config"

	"sigs.k8s.io/boskos/aws-janitor/account"
	"sigs.k8s.io/boskos/aws-janitor/regions"
	"sigs.k8s.io/boskos/aws-janitor/resources"
)

var (
	region                     = flag.String("region", "", "Region to list (defaults to all)")
	enableTargetGroupClean     = flag.Bool("enable-target-group-clean", false, "If true, clean target groups.")
	enableKeyPairsClean        = flag.Bool("enable-key-pairs-clean", false, "If true, clean key pairs.")
	enableVPCEndpointsClean    = flag.Bool("enable-vpc-endpoints-clean", false, "If true, clean vpc endpoints.")
	skipRoute53ManagementCheck = flag.Bool("skip-route53-management-check", false, "If true, skip managed zone check and managed resource name check.")
	enableDNSZoneClean         = flag.Bool("enable-dns-zone-clean", false, "If true, clean DNS zones.")
	enableS3BucketsClean       = flag.Bool("enable-s3-buckets-clean", false, "If true, clean S3 buckets.")
)

func listResources(res resources.Type, cfg aws2.Config, acct string, regions []string) {
	fmt.Printf("==%T==\n", res)
	for _, region := range regions {
		set, err := res.ListAll(resources.Options{
			Config:                     &cfg,
			Account:                    acct,
			Region:                     region,
			DryRun:                     true,
			EnableTargetGroupClean:     *enableTargetGroupClean,
			EnableKeyPairsClean:        *enableKeyPairsClean,
			EnableVPCEndpointsClean:    *enableVPCEndpointsClean,
			SkipRoute53ManagementCheck: *skipRoute53ManagementCheck,
			EnableDNSZoneClean:         *enableDNSZoneClean,
			EnableS3BucketsClean:       *enableS3BucketsClean,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error listing %T: %v\n", res, err)
			continue
		}

		for _, s := range set.GetARNs() {
			fmt.Println(s)
		}
	}
}

func main() {
	flag.Parse()

	cfg, err := configv2.LoadDefaultConfig(context.TODO())
	acct, err := account.GetAccount(cfg, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error retrieving account: %v\n", err)
		os.Exit(1)
	}

	regionList, err := regions.ParseRegion(&cfg, *region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing region: %v\n", err)
		os.Exit(1)
	}

	for _, r := range resources.RegionalTypeList {
		listResources(r, cfg, acct, regionList)
	}
	for _, r := range resources.GlobalTypeList {
		listResources(r, cfg, acct, []string{""})
	}
}
