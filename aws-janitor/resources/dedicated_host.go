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
	"context"
	"fmt"
	"strings"
	"time"

	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/pkg/errors"

	"github.com/sirupsen/logrus"
)

// Dedicated Hosts: https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeDedicatedHosts

type DedicatedHosts struct{}

func (DedicatedHosts) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	inp := &ec2v2.DescribeHostsInput{}

	var toDelete []string // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *ec2v2.DescribeHostsOutput, _ bool) bool {
		for _, _host := range page.Hosts {
			h := &host{
				Account: opts.Account,
				Region:  opts.Region,
				HostId:  *_host.HostId,
			}
			tags := fromEC2Tags(_host.Tags)

			if !set.Mark(opts, h, _host.AllocationTime, tags) {
				continue
			}

			logger.Warningf("%s: deleting %T: %s (%s)", h.ARN(), _host, h.HostId, tags[NameTagKey])
			if !opts.DryRun {
				toDelete = append(toDelete, *_host.HostId)
			}
		}
		return true
	}

	if err := DescribeHostsPages(svc, inp, pageFunc); err != nil {
		return err
	}

	if len(toDelete) > 0 {
		if _, err := svc.ReleaseHosts(context.TODO(), &ec2v2.ReleaseHostsInput{HostIds: toDelete}); err != nil {
			logger.Warningf("Release failed for Hosts: %s : %v", strings.Join(toDelete, ", "), err)
		}
	}
	return nil
}

func DescribeHostsPages(svc *ec2v2.Client, inp *ec2v2.DescribeHostsInput, pageFunc func(page *ec2v2.DescribeHostsOutput, _ bool) bool) error {
	paginator := ec2v2.NewDescribeHostsPaginator(svc, inp)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			logrus.Warningf("failed to get page, %v", err)
		} else {
			pageFunc(page, false)
		}
	}
	return nil
}

func (DedicatedHosts) ListAll(opts Options) (*Set, error) {

	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	inp := &ec2v2.DescribeHostsInput{}

	err := DescribeHostsPages(svc, inp, func(Hosts *ec2v2.DescribeHostsOutput, _ bool) bool {
		for _, _host := range Hosts.Hosts {
			now := time.Now()
			arn := instance{
				Account:    opts.Account,
				Region:     opts.Region,
				InstanceID: *_host.HostId,
			}.ARN()

			set.firstSeen[arn] = now
		}
		return true

	})
	return set, errors.Wrapf(err, "couldn't describe DedicatedHosts for %q in %q", opts.Account, opts.Region)
}

type host struct {
	Account string
	Region  string
	HostId  string
}

func (h host) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:host/%s", h.Region, h.Account, h.HostId)
}

func (h host) ResourceKey() string {
	return h.ARN()
}
