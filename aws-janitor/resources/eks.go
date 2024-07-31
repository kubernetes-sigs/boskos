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

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	eksv2 "github.com/aws/aws-sdk-go-v2/service/eks"
	eksv2types "github.com/aws/aws-sdk-go-v2/service/eks/types"

	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

type EKS struct{}

func (e EKS) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := eksv2.NewFromConfig(*opts.Config, func(opt *eksv2.Options) {
		opt.Region = opts.Region
	})

	var toDelete []*eksCluster
	err := e.describeClusters(opts, set, svc, func(cluster *eksCluster) {
		logger.Warningf("%s: deleting %T: %s", cluster.ARN(), eksv2types.Cluster{}, cluster.name)
		if !opts.DryRun {
			toDelete = append(toDelete, cluster)
		}
	})
	if err != nil {
		return err
	}

	for _, cluster := range toDelete {
		if ngErr := e.deleteNodegroupsForCluster(svc, cluster, logger); ngErr != nil {
			logger.Warningf("%s: skipping delete due to error deleting nodegroups: %v", cluster.ARN(), ngErr)
			continue
		}
		if fgpErr := e.deleteFargateProfilesForCluster(svc, cluster, logger); fgpErr != nil {
			logger.Warningf("%s: skipping delete due to error deleting fargate profiles: %v", cluster.ARN(), fgpErr)
			continue
		}
		if _, err := svc.DeleteCluster(context.TODO(), &eksv2.DeleteClusterInput{Name: aws2.String(cluster.name)}); err != nil {
			logger.Warningf("%s: delete failed: %v", cluster.ARN(), err)
		}
	}

	return nil
}

func (e EKS) ListAll(opts Options) (*Set, error) {
	svc := eksv2.NewFromConfig(*opts.Config, func(opt *eksv2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)
	err := e.describeClusters(opts, set, svc, func(_ *eksCluster) {})
	return set, err
}

func (EKS) describeClusters(opts Options, set *Set, svc *eksv2.Client, deleteFunc func(*eksCluster)) error {
	logger := logrus.WithField("options", opts)

	err := ListClustersPages(svc, &eksv2.ListClustersInput{},
		func(page *eksv2.ListClustersOutput, _ bool) bool {
			for _, clusterName := range page.Clusters {
				out, describeErr := svc.DescribeCluster(context.TODO(), &eksv2.DescribeClusterInput{Name: aws2.String(clusterName)})
				if describeErr != nil || out == nil {
					logger.Warningf("%s: failed to describe cluster: %v", clusterName, describeErr)
					continue
				}
				cluster := eksCluster{
					arn:  *out.Cluster.Arn,
					name: clusterName,
				}
				tags := make(Tags, len(out.Cluster.Tags))
				for k, v := range out.Cluster.Tags {
					tags.Add(aws2.String(k), aws2.String(v))
				}
				if !set.Mark(opts, cluster, out.Cluster.CreatedAt, tags) {
					continue
				}
				deleteFunc(&cluster)
			}

			return true
		})
	if err != nil {
		return err
	}

	return nil
}

func ListClustersPages(svc *eksv2.Client, input *eksv2.ListClustersInput, pageFunc func(page *eksv2.ListClustersOutput, _ bool) bool) error {
	paginator := eksv2.NewListClustersPaginator(svc, input)
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

func (EKS) deleteNodegroupsForCluster(svc *eksv2.Client, cluster *eksCluster, logger logrus.FieldLogger) error {
	var errs []error
	listErr := ListNodegroupsPages(svc, &eksv2.ListNodegroupsInput{ClusterName: aws2.String(cluster.name)},
		func(page *eksv2.ListNodegroupsOutput, _ bool) bool {
			for _, nodeGroup := range page.Nodegroups {
				if _, err := svc.DeleteNodegroup(context.TODO(), &eksv2.DeleteNodegroupInput{
					ClusterName:   aws2.String(cluster.name),
					NodegroupName: aws2.String(nodeGroup)}); err != nil {
					logger.Warningf("%s: failed to delete nodegroup %s: %v", cluster.ARN(), nodeGroup, err)
					errs = append(errs, err)
				}
			}
			return true
		})

	if listErr != nil {
		logger.Warningf("%s: failed to list nodegroups: %v", cluster.ARN(), listErr)
		errs = append(errs, listErr)
	}
	return kerrors.NewAggregate(errs)
}

func ListNodegroupsPages(svc *eksv2.Client, input *eksv2.ListNodegroupsInput, pageFunc func(page *eksv2.ListNodegroupsOutput, _ bool) bool) error {
	paginator := eksv2.NewListNodegroupsPaginator(svc, input)
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

func (EKS) deleteFargateProfilesForCluster(svc *eksv2.Client, cluster *eksCluster, logger logrus.FieldLogger) error {
	var errs []error
	listErr := ListFargateProfilesPages(svc, &eksv2.ListFargateProfilesInput{ClusterName: aws2.String(cluster.name)},
		func(page *eksv2.ListFargateProfilesOutput, _ bool) bool {
			for _, fargateProfile := range page.FargateProfileNames {
				if _, err := svc.DeleteFargateProfile(context.TODO(), &eksv2.DeleteFargateProfileInput{
					ClusterName:        aws2.String(cluster.name),
					FargateProfileName: aws2.String(fargateProfile)}); err != nil {
					logger.Warningf("%s: failed to delete fargate profile %s: %v", cluster.ARN(), fargateProfile, err)
					errs = append(errs, err)
				}
			}
			return true
		})

	if listErr != nil {
		logger.Warningf("%s: failed to list fargate profiles: %v", cluster.ARN(), listErr)
		errs = append(errs, listErr)
	}
	return kerrors.NewAggregate(errs)
}

func ListFargateProfilesPages(svc *eksv2.Client, input *eksv2.ListFargateProfilesInput, pageFunc func(page *eksv2.ListFargateProfilesOutput, _ bool) bool) error {
	paginator := eksv2.NewListFargateProfilesPaginator(svc, input)
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

type eksCluster struct {
	name string
	arn  string
}

func (c eksCluster) ARN() string {
	return c.arn
}

func (c eksCluster) ResourceKey() string {
	return c.ARN()
}
