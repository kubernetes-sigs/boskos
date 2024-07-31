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
	"time"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Clean-up ENIs

type NetworkInterfaces struct{}

func (NetworkInterfaces) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	var toDelete []*networkInterface // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *ec2v2.DescribeNetworkInterfacesOutput, _ bool) bool {
		for _, eni := range page.NetworkInterfaces {
			a := &networkInterface{Region: opts.Region, Account: opts.Account, ID: *eni.NetworkInterfaceId}
			var attachTime *time.Time = nil
			if eni.Attachment != nil {
				a.AttachmentID = *eni.Attachment.AttachmentId
				attachTime = eni.Attachment.AttachTime
			}
			tags := fromEC2Tags(eni.TagSet)
			// AttachTime isn't exactly the creation time, but it's better than nothing.
			if !set.Mark(opts, a, attachTime, tags) {
				continue
			}
			// Since tags and other metadata may not propagate to ENIs from their attachments,
			// we avoid deleting any ENI that is currently attached. Once their attached resource is deleted,
			// we can safely delete ENIs in a later clean-up run (using the mark data we saved in this run).
			if eni.Attachment != nil {
				continue
			}
			logger.Warningf("%s: deleting %T (%s)", a.ARN(), a, tags[NameTagKey])
			if !opts.DryRun {
				toDelete = append(toDelete, a)
			}
		}
		return true
	}

	if err := DescribeNetworkInterfacesPages(svc, &ec2v2.DescribeNetworkInterfacesInput{}, pageFunc); err != nil {
		return err
	}

	for _, eni := range toDelete {
		deleteInput := &ec2v2.DeleteNetworkInterfaceInput{
			NetworkInterfaceId: aws2.String(eni.ID),
		}

		if _, err := svc.DeleteNetworkInterface(context.TODO(), deleteInput); err != nil {
			logger.Warningf("%s: delete failed: %v", eni.ARN(), err)
		}
	}

	return nil
}

func (NetworkInterfaces) ListAll(opts Options) (*Set, error) {
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	input := &ec2v2.DescribeNetworkInterfacesInput{}

	err := DescribeNetworkInterfacesPages(svc, input, func(enis *ec2v2.DescribeNetworkInterfacesOutput, isLast bool) bool {
		now := time.Now()
		for _, eni := range enis.NetworkInterfaces {
			arn := networkInterface{
				Region:  opts.Region,
				Account: opts.Account,
				ID:      *eni.NetworkInterfaceId,
			}.ARN()
			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe network interfaces for %q in %q", opts.Account, opts.Region)
}

func DescribeNetworkInterfacesPages(svc *ec2v2.Client, input *ec2v2.DescribeNetworkInterfacesInput, pageFunc func(enis *ec2v2.DescribeNetworkInterfacesOutput, isLast bool) bool) error {
	paginator := ec2v2.NewDescribeNetworkInterfacesPaginator(svc, input)

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

type networkInterface struct {
	Region       string
	Account      string
	AttachmentID string
	ID           string
}

func (eni networkInterface) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:network-interface/%s", eni.Region, eni.Account, eni.ID)
}

func (eni networkInterface) ResourceKey() string {
	return eni.ARN()
}
