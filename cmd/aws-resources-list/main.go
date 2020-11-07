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
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/aws/aws-sdk-go/aws/session"
	"sigs.k8s.io/boskos/aws-janitor/account"
	"sigs.k8s.io/boskos/aws-janitor/regions"
	"sigs.k8s.io/boskos/aws-janitor/resources"
)

var (
	region = flag.String("region", "", "Region to list (defaults to all)")
)

func listResources(res resources.Type, sess *session.Session, acct string, regions []string) {
	fmt.Printf("==%T==\n", res)
	for _, region := range regions {
		set, err := res.ListAll(resources.Options{Session: sess, Account: acct, Region: region})
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

	session := session.Must(session.NewSession())
	acct, err := account.GetAccount(session, *region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error retrieving account: %v", err)
		os.Exit(1)
	}

	var regionList []string
	if *region == "" {
		regionList, err = regions.GetAll(session)
		if err != nil {
			fmt.Fprintf(os.Stderr, "couldn't retrieve list of regions: %v", err)
		}
		sort.Strings(regionList)
	} else {
		regionList = []string{*region}
	}

	for _, r := range resources.RegionalTypeList {
		listResources(r, session, acct, regionList)
	}
	for _, r := range resources.GlobalTypeList {
		listResources(r, session, acct, []string{""})
	}
}
