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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

type EKS struct{}

func (e EKS) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := eks.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	var toDelete []*eksCluster
	err := e.describeClusters(opts, set, svc, func(cluster *eksCluster) {
		logger.Warningf("%s: deleting %T: %s", cluster.ARN(), eks.Cluster{}, cluster.name)
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
		if _, err := svc.DeleteCluster(&eks.DeleteClusterInput{Name: aws.String(cluster.name)}); err != nil {
			logger.Warningf("%s: delete failed: %v", cluster.ARN(), err)
		}
	}

	return nil
}

func (e EKS) ListAll(opts Options) (*Set, error) {
	svc := eks.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(0)
	err := e.describeClusters(opts, set, svc, func(_ *eksCluster) {})
	return set, err
}

func (EKS) describeClusters(opts Options, set *Set, svc *eks.EKS, deleteFunc func(*eksCluster)) error {
	logger := logrus.WithField("options", opts)

	err := svc.ListClustersPages(&eks.ListClustersInput{},
		func(page *eks.ListClustersOutput, _ bool) bool {
			for _, clusterName := range page.Clusters {
				out, describeErr := svc.DescribeCluster(&eks.DescribeClusterInput{Name: clusterName})
				if describeErr != nil || out == nil {
					logger.Warningf("%s: failed to describe cluster: %v", aws.StringValue(clusterName), describeErr)
					continue
				}
				cluster := eksCluster{
					arn:  aws.StringValue(out.Cluster.Arn),
					name: aws.StringValue(clusterName),
				}
				tags := make([]Tag, len(out.Cluster.Tags))
				for k, v := range out.Cluster.Tags {
					tags = append(tags, NewTag(aws.String(k), v))
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

func (EKS) deleteNodegroupsForCluster(svc *eks.EKS, cluster *eksCluster, logger logrus.FieldLogger) error {
	var errs []error
	listErr := svc.ListNodegroupsPages(&eks.ListNodegroupsInput{ClusterName: aws.String(cluster.name)},
		func(page *eks.ListNodegroupsOutput, _ bool) bool {
			for _, nodeGroup := range page.Nodegroups {
				if _, err := svc.DeleteNodegroup(&eks.DeleteNodegroupInput{
					ClusterName:   aws.String(cluster.name),
					NodegroupName: nodeGroup}); err != nil {
					logger.Warningf("%s: failed to delete nodegroup %s: %v", cluster.ARN(), aws.StringValue(nodeGroup), err)
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

func (EKS) deleteFargateProfilesForCluster(svc *eks.EKS, cluster *eksCluster, logger logrus.FieldLogger) error {
	var errs []error
	listErr := svc.ListFargateProfilesPages(&eks.ListFargateProfilesInput{ClusterName: aws.String(cluster.name)},
		func(page *eks.ListFargateProfilesOutput, _ bool) bool {
			for _, fargateProfile := range page.FargateProfileNames {
				if _, err := svc.DeleteFargateProfile(&eks.DeleteFargateProfileInput{
					ClusterName:        aws.String(cluster.name),
					FargateProfileName: fargateProfile}); err != nil {
					logger.Warningf("%s: failed to delete fargate profile %s: %v", cluster.ARN(), aws.StringValue(fargateProfile), err)
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
