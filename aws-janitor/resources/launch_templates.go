/*
Copyright 2020 The Kubernetes Authors.

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

// LaunchTemplates https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#EC2.DescribeLaunchTemplates
type LaunchTemplates struct{}

func (LaunchTemplates) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})

	var toDelete []*launchTemplate // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *ec2v2.DescribeLaunchTemplatesOutput, _ bool) bool {
		for _, lt := range page.LaunchTemplates {
			l := &launchTemplate{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      *lt.LaunchTemplateId,
				Name:    *lt.LaunchTemplateName,
			}
			if !set.Mark(opts, l, lt.CreateTime, fromEC2Tags(lt.Tags)) {
				continue
			}
			logger.Warningf("%s: deleting %T: %s", l.ARN(), lt, l.Name)
			if !opts.DryRun {
				toDelete = append(toDelete, l)
			}
		}
		return true
	}

	if err := DescribeLaunchTemplatesPages(svc, &ec2v2.DescribeLaunchTemplatesInput{}, pageFunc); err != nil {
		return err
	}

	for _, lt := range toDelete {
		deleteReq := &ec2v2.DeleteLaunchTemplateInput{
			LaunchTemplateId: aws2.String(lt.ID),
		}

		if _, err := svc.DeleteLaunchTemplate(context.TODO(), deleteReq); err != nil {
			logger.Warningf("%s: delete failed: %v", lt.ARN(), err)
		}
	}

	return nil
}

func (LaunchTemplates) ListAll(opts Options) (*Set, error) {
	svc := ec2v2.NewFromConfig(*opts.Config, func(opt *ec2v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	input := &ec2v2.DescribeLaunchTemplatesInput{}

	err := DescribeLaunchTemplatesPages(svc, input, func(lts *ec2v2.DescribeLaunchTemplatesOutput, isLast bool) bool {
		now := time.Now()
		for _, lt := range lts.LaunchTemplates {
			arn := launchTemplate{
				Account: opts.Account,
				Region:  opts.Region,
				ID:      *lt.LaunchTemplateId,
				Name:    *lt.LaunchTemplateName,
			}.ARN()
			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't list launch templates for %q in %q", opts.Account, opts.Region)
}

func DescribeLaunchTemplatesPages(svc *ec2v2.Client, input *ec2v2.DescribeLaunchTemplatesInput, pageFunc func(lts *ec2v2.DescribeLaunchTemplatesOutput, isLast bool) bool) error {
	paginator := ec2v2.NewDescribeLaunchTemplatesPaginator(svc, input)

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

type launchTemplate struct {
	Account string
	Region  string
	ID      string
	Name    string
}

func (lt launchTemplate) ARN() string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:launch-template/%s", lt.Region, lt.Account, lt.ID)
}

func (lt launchTemplate) ResourceKey() string {
	return lt.ARN()
}
