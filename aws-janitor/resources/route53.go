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
	"fmt"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Route53

type Route53ResourceRecordSets struct{}

// zoneIsManaged checks if the zone should be managed (and thus have records deleted) by us
func zoneIsManaged(z *route53.HostedZone) bool {
	// TODO: Move to a tag on the zone?
	name := aws.StringValue(z.Name)
	if "test-cncf-aws.k8s.io." == name {
		return true
	}

	logrus.Infof("unknown zone %q; ignoring", name)
	return false
}

var managedNameRegexes = []*regexp.Regexp{
	// e.g. api.e2e-71149fffac-dba53.test-cncf-aws.k8s.io.
	regexp.MustCompile(`^api\.e2e-[0-9a-z]{1,10}-[0-9a-f]{5}\.`),

	// e.g. api.internal.e2e-71149fffac-dba53.test-cncf-aws.k8s.io.
	regexp.MustCompile(`^api\.internal\.e2e-[0-9a-z]{1,10}-[0-9a-f]{5}\.`),

	// e.g. etcd-b.internal.e2e-71149fffac-dba53.test-cncf-aws.k8s.io.
	regexp.MustCompile(`^etcd-[a-z]\.internal\.e2e-[0-9a-z]{1,10}-[0-9a-f]{5}\.`),

	// e.g. etcd-events-b.internal.e2e-71149fffac-dba53.test-cncf-aws.k8s.io.
	regexp.MustCompile(`^etcd-events-[a-z]\.internal\.e2e-[0-9a-z]{1,10}-[0-9a-f]{5}\.`),
}

// resourceRecordSetIsManaged checks if the resource record should be managed (and thus deleted) by us
func resourceRecordSetIsManaged(rrs *route53.ResourceRecordSet) bool {
	if "A" != aws.StringValue(rrs.Type) {
		return false
	}

	name := aws.StringValue(rrs.Name)

	for _, managedNameRegex := range managedNameRegexes {
		if managedNameRegex.MatchString(name) {
			return true
		}
	}

	logrus.Infof("Ignoring unmanaged name %q", name)
	return false
}

// route53ResourceRecordSetsForZone marks all ResourceRecordSets in the provided zone and returns a slice containing those that should be deleted.
func route53ResourceRecordSetsForZone(opts Options, logger logrus.FieldLogger, svc *route53.Route53, zone *route53.HostedZone, zoneTags []Tag, set *Set) ([]*route53ResourceRecordSet, error) {
	var toDelete []*route53ResourceRecordSet

	recordsPageFunc := func(records *route53.ListResourceRecordSetsOutput, _ bool) bool {
		for _, rrs := range records.ResourceRecordSets {
			if !resourceRecordSetIsManaged(rrs) {
				continue
			}

			o := &route53ResourceRecordSet{zone: zone, obj: rrs}
			// no tags for ResourceRecordSets, so use zone tags instead
			if !set.Mark(opts, o, nil, zoneTags) {
				continue
			}
			logger.Warningf("%s: deleting %T: %s", o.ARN(), rrs, *rrs.Name)
		}
		return true
	}

	err := svc.ListResourceRecordSetsPages(&route53.ListResourceRecordSetsInput{HostedZoneId: zone.Id}, recordsPageFunc)
	if err != nil {
		return nil, err
	}
	return toDelete, nil
}

func (rrs Route53ResourceRecordSets) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)
	svc := route53.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))

	var zones []*route53.HostedZone
	zoneTags := make(map[string][]Tag)

	pageFunc := func(zs *route53.ListHostedZonesOutput, _ bool) bool {
		// Because route53 has such low rate limits, we collect the changes per-zone, to minimize API calls
		for _, z := range zs.HostedZones {
			if !zoneIsManaged(z) {
				continue
			}

			zones = append(zones, z)
			zoneTags[aws.StringValue(z.Id)] = nil
		}

		return true
	}

	if err := svc.ListHostedZonesPages(&route53.ListHostedZonesInput{}, pageFunc); err != nil {
		return err
	}

	if err := rrs.fetchZoneTags(zoneTags, svc); err != nil {
		return err
	}

	for _, zone := range zones {
		toDelete, err := route53ResourceRecordSetsForZone(opts, logger, svc, zone, zoneTags[aws.StringValue(zone.Id)], set)
		if err != nil {
			return err
		}
		if opts.DryRun {
			continue
		}

		var changes []*route53.Change
		for _, rrs := range toDelete {
			change := &route53.Change{
				Action:            aws.String(route53.ChangeActionDelete),
				ResourceRecordSet: rrs.obj,
			}

			changes = append(changes, change)
		}

		for len(changes) != 0 {
			// Limit of 1000 changes per request
			chunk := changes
			if len(chunk) > 1000 {
				chunk = chunk[:1000]
				changes = changes[1000:]
			} else {
				changes = nil
			}

			logger.Infof("Deleting %d route53 resource records", len(chunk))
			deleteReq := &route53.ChangeResourceRecordSetsInput{
				HostedZoneId: zone.Id,
				ChangeBatch:  &route53.ChangeBatch{Changes: chunk},
			}

			if _, err := svc.ChangeResourceRecordSets(deleteReq); err != nil {
				logger.Warningf("unable to delete DNS records: %v", err)
			}
		}
	}

	return nil
}

func (Route53ResourceRecordSets) ListAll(opts Options) (*Set, error) {
	svc := route53.New(opts.Session, aws.NewConfig().WithRegion(opts.Region))
	set := NewSet(0)

	var rrsErr error
	err := svc.ListHostedZonesPages(&route53.ListHostedZonesInput{}, func(zones *route53.ListHostedZonesOutput, _ bool) bool {
		for _, z := range zones.HostedZones {
			zone := z
			if !zoneIsManaged(zone) {
				continue
			}
			inp := &route53.ListResourceRecordSetsInput{HostedZoneId: z.Id}
			err := svc.ListResourceRecordSetsPages(inp, func(recordSets *route53.ListResourceRecordSetsOutput, _ bool) bool {
				now := time.Now()
				for _, recordSet := range recordSets.ResourceRecordSets {
					arn := route53ResourceRecordSet{
						account: opts.Account,
						region:  opts.Region,
						zone:    zone,
						obj:     recordSet,
					}.ARN()
					set.firstSeen[arn] = now
				}
				return true
			})
			if err != nil {
				rrsErr = errors.Wrapf(err, "couldn't describe route53 resources for %q in %q zone %q", opts.Account, opts.Region, *z.Id)
				return false
			}

		}
		return true
	})

	if rrsErr != nil {
		return set, rrsErr
	}
	return set, errors.Wrapf(err, "couldn't describe route53 instance profiles for %q in %q", opts.Account, opts.Region)

}

func (rrs Route53ResourceRecordSets) fetchZoneTags(zoneTags map[string][]Tag, svc *route53.Route53) error {
	return incrementalFetchTags(zoneTags, 10, func(ids []*string) error {
		output, err := svc.ListTagsForResources(&route53.ListTagsForResourcesInput{
			ResourceType: aws.String(route53.TagResourceTypeHostedzone),
			ResourceIds:  ids,
		})
		if err != nil {
			return err
		}
		for _, rts := range output.ResourceTagSets {
			rtype := aws.StringValue(rts.ResourceType)
			if rtype != route53.TagResourceTypeHostedzone {
				return fmt.Errorf("invalid type in ListTagsForResources output: %s", rtype)
			}
			id := aws.StringValue(rts.ResourceId)
			_, ok := zoneTags[id]
			if !ok {
				return fmt.Errorf("unknown zone id in ListTagsForResources output: %s", id)
			}
			for _, tag := range rts.Tags {
				zoneTags[id] = append(zoneTags[id], NewTag(tag.Key, tag.Value))
			}
		}
		return nil
	})
}

type route53ResourceRecordSet struct {
	account string
	region  string
	zone    *route53.HostedZone
	obj     *route53.ResourceRecordSet
}

func (r route53ResourceRecordSet) ARN() string {
	return fmt.Sprintf("arn:aws:route53:%s:%s:%s/%s/%s", r.region, r.account, aws.StringValue(r.obj.Type), aws.StringValue(r.zone.Id), aws.StringValue(r.obj.Name))
}

func (r route53ResourceRecordSet) ResourceKey() string {
	return r.ARN()
}
