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
	"context"
	"fmt"
	"strings"
	"time"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	iamv2 "github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/sirupsen/logrus"
)

// IAM OIDC Providers

type IAMOIDCProviders struct{}

func fetchOIDCProviderAndTags(ctx context.Context, svc *iamv2.Client, arn string) (*iamv2.GetOpenIDConnectProviderOutput, Tags, error) {
	oidcProvider, err := svc.GetOpenIDConnectProvider(ctx, &iamv2.GetOpenIDConnectProviderInput{OpenIDConnectProviderArn: &arn})
	if err != nil {
		return nil, nil, fmt.Errorf("error from GetOpenIDConnectProvider: %w", err)
	}

	// Fetch the tags (with pagination)
	tags := make(Tags)
	tagsRequest := &iamv2.ListOpenIDConnectProviderTagsInput{OpenIDConnectProviderArn: &arn}
	for {
		response, err := svc.ListOpenIDConnectProviderTags(ctx, tagsRequest)
		if err != nil {
			return nil, nil, fmt.Errorf("error from ListOpenIDConnectProviderTags: %w", err)
		}
		for _, t := range response.Tags {
			tags.Add(t.Key, t.Value)
		}
		if !response.IsTruncated {
			break
		}
		tagsRequest.Marker = response.Marker
	}

	return oidcProvider, tags, nil
}

// oidcProviderIsManaged checks if the OIDC provider should be managed (and thus deleted) by us.
func oidcProviderIsManaged(_oidcProvider *iamv2.GetOpenIDConnectProviderOutput, tags Tags) bool {
	// Look for one of the kubernetes cluster ownership tags
	for k := range tags {
		if strings.HasPrefix(k, "kubernetes.io/cluster/") ||
			strings.HasPrefix(k, "alpha.eksctl.io/cluster-name") ||
			k == "KubernetesCluster" {
			return true
		}
	}
	return false
}

func (IAMOIDCProviders) MarkAndSweep(opts Options, set *Set) error {
	ctx := context.TODO()

	logger := logrus.WithField("options", opts)
	svc := iamv2.NewFromConfig(*opts.Config, func(opt *iamv2.Options) {
		opt.Region = opts.Region
	})

	var toDelete []*iamOIDCProvider

	providers, err := svc.ListOpenIDConnectProviders(ctx, &iamv2.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return fmt.Errorf("error from ListOpenIDConnectProviders: %w", err)
	}

	for _, oidcProvider := range providers.OpenIDConnectProviderList {
		arn := *oidcProvider.Arn
		oidcProvider, tags, err := fetchOIDCProviderAndTags(ctx, svc, arn)
		if err != nil {
			logger.Warningf("failed fetching oidcProvider and tags: %v", err)
			continue
		}
		if !oidcProviderIsManaged(oidcProvider, tags) {
			logger.Warningf("oidcProvider %s is not managed (tags=%v)", arn, tags)
			continue
		}
		l := &iamOIDCProvider{arn: arn}
		if set.Mark(opts, l, oidcProvider.CreateDate, tags) {
			logger.Warningf("%s: deleting url=%s", arn, *oidcProvider.Url)
			if !opts.DryRun {
				toDelete = append(toDelete, l)
			}
		}
	}

	for _, r := range toDelete {
		if err := r.delete(ctx, svc, logger); err != nil {
			logger.Warningf("%s: delete failed: %v", r.ARN(), err)
		}
	}

	return nil
}

func (IAMOIDCProviders) ListAll(opts Options) (*Set, error) {
	ctx := context.TODO()

	svc := iamv2.NewFromConfig(*opts.Config, func(opt *iamv2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)

	providers, err := svc.ListOpenIDConnectProviders(ctx, &iamv2.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return nil, fmt.Errorf("error from ListOpenIDConnectProvidersWithContext: %w", err)
	}

	now := time.Now()
	for _, oidcProvider := range providers.OpenIDConnectProviderList {
		arn := iamOIDCProvider{
			arn: *oidcProvider.Arn,
		}.ARN()

		set.firstSeen[arn] = now
	}

	return set, nil
}

type iamOIDCProvider struct {
	arn string
}

func (r iamOIDCProvider) ARN() string {
	return r.arn
}

func (r iamOIDCProvider) ResourceKey() string {
	return r.ARN()
}

func (r iamOIDCProvider) delete(ctx context.Context, svc *iamv2.Client, logger logrus.FieldLogger) error {
	logger.Debugf("deleting OIDC Provider %q", r.arn)

	req := &iamv2.DeleteOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: aws2.String(r.arn),
	}
	if _, err := svc.DeleteOpenIDConnectProvider(ctx, req); err != nil {
		return fmt.Errorf("error from DeleteOpenIDConnectProvider: %w", err)
	}

	return nil
}
