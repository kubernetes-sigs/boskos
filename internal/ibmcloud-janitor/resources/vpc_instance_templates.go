/*
Copyright 2026 The Kubernetes Authors.

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
	"github.com/IBM/vpc-go-sdk/vpcv1"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type VPCInstanceTemplate struct{}

// Cleans up the instance templates in the target VPC.
func (VPCInstanceTemplate) cleanup(options *CleanupOptions) error {
	resourceLogger := logrus.WithFields(logrus.Fields{"resource": options.Resource.Name})
	resourceLogger.Info("Cleaning up the instance templates")
	client, err := NewVPCClient(options)
	if err != nil {
		return errors.Wrap(err, "couldn't create VPC client")
	}

	targetVPCIDs, err := instanceTemplateTargetVPCIDs(client)
	if err != nil {
		return err
	}

	templateList, _, err := client.ListInstanceTemplates(&vpcv1.ListInstanceTemplatesOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to list the instance templates")
	}

	for _, templateIntf := range templateList.Templates {
		template, ok := templateIntf.(*vpcv1.InstanceTemplate)
		if !ok {
			continue
		}
		if !instanceTemplateBelongsToVPC(template, targetVPCIDs) {
			continue
		}
		_, err := client.DeleteInstanceTemplate(&vpcv1.DeleteInstanceTemplateOptions{
			ID: template.ID,
		})
		if err != nil {
			return errors.Wrapf(err, "failed to delete the instance template %q", *template.Name)
		}
		resourceLogger.WithFields(logrus.Fields{"name": *template.Name}).Info("Successfully deleted the instance template")
	}
	resourceLogger.Info("Successfully deleted the instance templates")
	return nil
}

func instanceTemplateTargetVPCIDs(client *IBMVPCClient) (map[string]struct{}, error) {
	targetVPCIDs := make(map[string]struct{})
	if client.VPCID != "" {
		targetVPCIDs[client.VPCID] = struct{}{}
		return targetVPCIDs, nil
	}

	// ResourceGroupID is required by GetVPCResourceData; without vpc-id, it defines legacy cleanup scope.
	vpcs, _, err := client.ListVpcs(&vpcv1.ListVpcsOptions{
		ResourceGroupID: &client.ResourceGroupID,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list VPCs for instance template cleanup")
	}
	for _, vpc := range vpcs.Vpcs {
		if vpc.ID != nil {
			targetVPCIDs[*vpc.ID] = struct{}{}
		}
	}
	return targetVPCIDs, nil
}

func instanceTemplateBelongsToVPC(template *vpcv1.InstanceTemplate, targetVPCIDs map[string]struct{}) bool {
	var id *string
	switch vpc := template.VPC.(type) {
	case *vpcv1.VPCIdentity:
		id = vpc.ID
	case *vpcv1.VPCIdentityByID:
		id = vpc.ID
	default:
		return false
	}
	if id == nil {
		return false
	}
	_, ok := targetVPCIDs[*id]
	return ok
}
