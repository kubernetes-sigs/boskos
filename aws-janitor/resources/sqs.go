/*
Copyright 2021 The Kubernetes Authors.

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
	"strconv"
	"time"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	eventbridgev2 "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	sqsv2 "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqsv2types "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// SQS queues: https://docs.aws.amazon.com/sdk-for-go/api/service/sqs

type SQSQueues struct{}

func (SQSQueues) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := sqsv2.NewFromConfig(*opts.Config, func(opt *sqsv2.Options) {
		opt.Region = opts.Region
	})

	input := &sqsv2.ListQueuesInput{}

	var toDelete []*sqsQueue // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *sqsv2.ListQueuesOutput, _ bool) bool {
		for _, url := range page.QueueUrls {
			url := url
			attrInput := &sqsv2.GetQueueAttributesInput{
				AttributeNames: []sqsv2types.QueueAttributeName{sqsv2types.QueueAttributeNameAll},
				QueueUrl:       &url,
			}
			attr, err := svc.GetQueueAttributes(context.TODO(), attrInput)
			if err != nil {
				return false
			}

			q := &sqsQueue{
				Account:  opts.Account,
				Region:   opts.Region,
				Name:     attr.Attributes[string(sqsv2types.QueueAttributeNameQueueArn)],
				QueueURL: url,
			}
			unixTimestamp, _ := strconv.ParseInt(attr.Attributes[string(sqsv2types.QueueAttributeNameCreatedTimestamp)], 10, 64)
			creationTime := time.Unix(unixTimestamp, 0)

			tagResp, err := svc.ListQueueTags(context.TODO(), &sqsv2.ListQueueTagsInput{QueueUrl: &url})
			if err != nil {
				logger.Warningf("%s: failed listing tags: %v", q.ARN(), err)
				return false
			}
			tags := make(Tags, len(tagResp.Tags))
			for k, v := range tagResp.Tags {
				tags.Add(aws2.String(k), aws2.String(v))
			}
			if !set.Mark(opts, q, &creationTime, tags) {
				continue
			}

			logger.Warningf("%s: deleting %T: %s", q.ARN(), url, q.Name)
			if !opts.DryRun {
				toDelete = append(toDelete, q)
			}

			svcRules := eventbridgev2.NewFromConfig(*opts.Config, func(opt *eventbridgev2.Options) {
				opt.Region = opts.Region
			})

			// Only delete rules that uses SQS queue as target. There are default rules that should not be deleted.
			rules, err := svcRules.ListRuleNamesByTarget(context.TODO(), &eventbridgev2.ListRuleNamesByTargetInput{
				TargetArn: aws2.String(attr.Attributes[string(sqsv2types.QueueAttributeNameQueueArn)]),
			})
			if err != nil {
				logger.Warningf("listing rules by target failed: %s", err.Error())
			}

			for i := range rules.RuleNames {
				deleteEventBridgeRule(&rules.RuleNames[i], svcRules, logger)
			}

		}
		return true
	}

	if err := ListQueuesPages(svc, input, pageFunc); err != nil {
		return err
	}
	for _, q := range toDelete {
		_, err := svc.DeleteQueue(context.TODO(), &sqsv2.DeleteQueueInput{QueueUrl: aws2.String(q.QueueURL)})
		if err != nil {
			var doesNotExistError sqsv2types.QueueDoesNotExist
			if errors.As(err, &doesNotExistError) {
				continue
			}
			logger.Warningf("%s: delete failed: %v", q.ARN(), err)
		}
	}
	return nil
}

func ListQueuesPages(svc *sqsv2.Client, input *sqsv2.ListQueuesInput, pageFunc func(page *sqsv2.ListQueuesOutput, _ bool) bool) error {
	paginator := sqsv2.NewListQueuesPaginator(svc, input)

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

func deleteEventBridgeRule(rule *string, svcRules *eventbridgev2.Client, logger *logrus.Entry) {
	// Before removing a rule, all the target must be removed from the rule.
	// For removing the targets, target ids must be provided.
	targets, err := svcRules.ListTargetsByRule(context.TODO(), &eventbridgev2.ListTargetsByRuleInput{
		Rule: rule,
	})
	if err != nil {
		logger.Warningf("%s: listing targets failed: %v", *rule, err)
	}
	targetStr := make([]string, 0)
	for _, t := range targets.Targets {
		targetStr = append(targetStr, *t.Id)
	}

	_, err = svcRules.RemoveTargets(context.TODO(), &eventbridgev2.RemoveTargetsInput{
		Rule: rule,
		Ids:  targetStr,
	})
	if err != nil {
		logger.Warningf("%s: removing targets failed: %v", *rule, err)
	}

	deleteInput := &eventbridgev2.DeleteRuleInput{
		Name:  rule,
		Force: true,
	}
	if _, err := svcRules.DeleteRule(context.TODO(), deleteInput); err != nil {
		logger.Warningf("%s: delete failed: %v", *rule, err)
	}
}

func (SQSQueues) ListAll(opts Options) (*Set, error) {
	svc := sqsv2.NewFromConfig(*opts.Config, func(opt *sqsv2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	input := &sqsv2.ListQueuesInput{}

	err := ListQueuesPages(svc, input, func(queues *sqsv2.ListQueuesOutput, _ bool) bool {
		for _, url := range queues.QueueUrls {
			url := url
			attrInput := &sqsv2.GetQueueAttributesInput{
				AttributeNames: []sqsv2types.QueueAttributeName{sqsv2types.QueueAttributeNameAll},
				QueueUrl:       &url,
			}
			attr, err := svc.GetQueueAttributes(context.TODO(), attrInput)
			if err != nil {
				return false
			}
			now := time.Now()
			arn := sqsQueue{
				Account:  opts.Account,
				Region:   opts.Region,
				Name:     attr.Attributes[string(sqsv2types.QueueAttributeNameQueueArn)],
				QueueURL: url,
			}.ARN()
			set.firstSeen[arn] = now
		}
		return true
	})
	return set, errors.Wrapf(err, "couldn't describe sqs queues for %q in %q", opts.Account, opts.Region)
}

type sqsQueue struct {
	Account  string
	Region   string
	Name     string
	QueueURL string
}

func (i sqsQueue) ARN() string {
	// arn:aws:sqs:us-west-1:111111111111:name
	return fmt.Sprintf("arn:aws:sqs:%s:%s:%s", i.Region, i.Account, i.Name)
}

func (i sqsQueue) ResourceKey() string {
	return i.ARN()
}
