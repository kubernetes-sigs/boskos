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
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// IAM Instance Profiles
type IAMInstanceProfiles struct{}

func (IAMInstanceProfiles) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := iam.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	var toDelete []*iamInstanceProfile // Paged call, defer deletion until we have the whole list.

	pageFunc := func(page *iam.ListInstanceProfilesOutput, _ bool) bool {
		for _, p := range page.InstanceProfiles {
			// We treat an instance profile as managed if all its roles are
			managed := true
			if len(p.Roles) == 0 {
				// Just in case...
				managed = false
			}

			var lastUsed time.Time
			for _, r := range p.Roles {
				if !roleIsManaged(r) {
					managed = false
					break
				}
				role, tags, err := fetchRoleAndTags(svc, r.RoleName)
				if err != nil {
					logger.Warningf("failed fetching role and tags: %v", err)
					managed = false
					break
				}
				if !opts.ManagedPerTags(tags) {
					managed = false
					break
				}
				if role.RoleLastUsed != nil && role.RoleLastUsed.LastUsedDate != nil && role.RoleLastUsed.LastUsedDate.After(lastUsed) {
					lastUsed = *role.RoleLastUsed.LastUsedDate
				}
			}

			if !managed {
				logger.Infof("%s: ignoring unmanaged profile", aws.StringValue(p.Arn))
				continue
			}

			o := &iamInstanceProfile{profile: p}
			// No tags for instance profiles
			if set.Mark(opts, o, p.CreateDate, nil) {
				if time.Since(lastUsed) < set.ttl {
					logger.Debugf("%s: used too recently, skipping", o.ARN())
					continue
				}
				logger.Warningf("%s: deleting %T: %s", o.ARN(), o, aws.StringValue(p.InstanceProfileName))
				if !opts.DryRun {
					toDelete = append(toDelete, o)
				}
			}
		}
		return true
	}

	if err := svc.ListInstanceProfilesPages(&iam.ListInstanceProfilesInput{}, pageFunc); err != nil {
		return err
	}

	for _, o := range toDelete {
		if err := o.delete(svc); err != nil {
			logger.Warningf("%s: delete failed: %v", o.ARN(), err)
		}
	}
	return nil
}

func (IAMInstanceProfiles) ListAll(opts Options) (*Set, error) {
	svc := iam.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(opts.DefaultTTL)
	inp := &iam.ListInstanceProfilesInput{}

	err := svc.ListInstanceProfilesPages(inp, func(profiles *iam.ListInstanceProfilesOutput, _ bool) bool {
		now := time.Now()
		for _, profile := range profiles.InstanceProfiles {
			arn := iamInstanceProfile{
				profile: profile,
			}.ARN()

			set.firstSeen[arn] = now
		}

		return true
	})

	return set, errors.Wrapf(err, "couldn't describe iam instance profiles for %q in %q", opts.Account, opts.Region)
}

type iamInstanceProfile struct {
	profile *iam.InstanceProfile
}

func (p iamInstanceProfile) ARN() string {
	return aws.StringValue(p.profile.Arn)
}

func (p iamInstanceProfile) ResourceKey() string {
	return aws.StringValue(p.profile.InstanceProfileId) + "::" + p.ARN()
}

func (p iamInstanceProfile) delete(svc *iam.IAM) error {
	// Unlink the roles first, before we can delete the instance profile.
	for _, role := range p.profile.Roles {
		request := &iam.RemoveRoleFromInstanceProfileInput{
			InstanceProfileName: p.profile.InstanceProfileName,
			RoleName:            role.RoleName,
		}

		if _, err := svc.RemoveRoleFromInstanceProfile(request); err != nil {
			return errors.Wrapf(err, "error removing role %q", aws.StringValue(role.RoleName))
		}
	}

	// Delete the instance profile.
	request := &iam.DeleteInstanceProfileInput{
		InstanceProfileName: p.profile.InstanceProfileName,
	}

	if _, err := svc.DeleteInstanceProfile(request); err != nil {
		return err
	}

	return nil
}
