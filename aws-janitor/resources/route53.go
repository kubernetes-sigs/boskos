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
	"regexp"
	"strings"
	"time"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	route53v2 "github.com/aws/aws-sdk-go-v2/service/route53"
	route53v2types "github.com/aws/aws-sdk-go-v2/service/route53/types"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Route53

type Route53ResourceRecordSets struct{}

// zoneIsManaged checks if the zone should be managed (and thus have records deleted) by us
func zoneIsManaged(z *route53v2types.HostedZone) bool {
	// TODO: Move to a tag on the zone?
	name := *z.Name
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

	// e.g. main.etcd.e2e-71149fffac-dba53.test-cncf-aws.k8s.io.
	regexp.MustCompile(`^main\.etcd\.e2e-[0-9a-z]{1,10}-[0-9a-f]{5}\.`),

	// e.g. events.etcd.e2e-71149fffac-dba53.test-cncf-aws.k8s.io.
	regexp.MustCompile(`^events\.etcd\.e2e-[0-9a-z]{1,10}-[0-9a-f]{5}\.`),

	// e.g. kops-controller.internal.e2e-71149fffac-dba53.test-cncf-aws.k8s.io.
	regexp.MustCompile(`^kops-controller\.internal\.e2e-[0-9a-z]{1,10}-[0-9a-f]{5}\.`),
}

// resourceRecordSetIsManaged checks if the resource record should be managed (and thus deleted) by us
func resourceRecordSetIsManaged(rrs *route53v2types.ResourceRecordSet) bool {
	if "A" != rrs.Type {
		return false
	}

	name := *rrs.Name

	for _, managedNameRegex := range managedNameRegexes {
		if managedNameRegex.MatchString(name) {
			return true
		}
	}

	logrus.Infof("Ignoring unmanaged name %q", name)
	return false
}

// route53ResourceRecordSetsForZone marks all ResourceRecordSets in the provided zone and returns a slice containing those that should be deleted.
func route53ResourceRecordSetsForZone(opts Options, logger logrus.FieldLogger, svc *route53v2.Client, zone *route53v2types.HostedZone, zoneTags Tags, set *Set) ([]*route53ResourceRecordSet, error) {
	var toDelete []*route53ResourceRecordSet

	recordsPageFunc := func(records *route53v2.ListResourceRecordSetsOutput, _ bool) bool {
		for _, rrs := range records.ResourceRecordSets {
			if !opts.SkipRoute53ManagementCheck && !resourceRecordSetIsManaged(&rrs) {
				continue
			}

			o := &route53ResourceRecordSet{zone: zone, obj: &rrs}
			// no tags for ResourceRecordSets, so use zone tags instead
			if !set.Mark(opts, o, nil, zoneTags) {
				continue
			}
			if _, ok := opts.SkipResourceRecordSetTypes[string(rrs.Type)]; !ok {
				logger.Warningf("%s: deleting %T: %s", o.ARN(), rrs, *rrs.Name)
				if !opts.DryRun {
					toDelete = append(toDelete, o)
				}
			}
		}
		return true
	}

	err := ListResourceRecordSetsPages(svc, &route53v2.ListResourceRecordSetsInput{HostedZoneId: zone.Id}, recordsPageFunc)
	if err != nil {
		return nil, err
	}
	return toDelete, nil
}

func ListResourceRecordSetsPages(svc *route53v2.Client, input *route53v2.ListResourceRecordSetsInput, pageFunc func(records *route53v2.ListResourceRecordSetsOutput, _ bool) bool) error {
	paginator := route53v2.NewListResourceRecordSetsPaginator(svc, input)
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

func (rrs Route53ResourceRecordSets) MarkAndSweep(opts Options, set *Set) error {
	logger := logrus.WithField("options", opts)

	svc := route53v2.NewFromConfig(*opts.Config, func(opt *route53v2.Options) {
		opt.Region = opts.Region
	})

	var zones []*route53v2types.HostedZone
	zoneTags := make(map[string]Tags)

	pageFunc := func(zs *route53v2.ListHostedZonesOutput, _ bool) bool {
		// Because route53 has such low rate limits, we collect the changes per-zone, to minimize API calls
		for _, z := range zs.HostedZones {
			if !opts.SkipRoute53ManagementCheck && !zoneIsManaged(&z) {
				continue
			}

			// ListHostedZones returns "/hostedzone/ABCDEF12345678" but other Route53 endpoints expect "ABCDEF12345678"
			z.Id = aws2.String(strings.TrimPrefix(*z.Id, "/hostedzone/"))

			zones = append(zones, &z)
			zoneTags[*z.Id] = nil
		}

		return true
	}

	if err := ListHostedZonesPages(svc, &route53v2.ListHostedZonesInput{}, pageFunc); err != nil {
		return err
	}

	if err := rrs.fetchZoneTags(zoneTags, svc); err != nil {
		return err
	}

	for _, zone := range zones {
		toDelete, err := route53ResourceRecordSetsForZone(opts, logger, svc, zone, zoneTags[*zone.Id], set)
		if err != nil {
			return err
		}
		if opts.DryRun {
			continue
		}

		var changes []route53v2types.Change
		for _, rrs := range toDelete {
			change := &route53v2types.Change{
				Action:            route53v2types.ChangeActionDelete,
				ResourceRecordSet: rrs.obj,
			}

			changes = append(changes, *change)
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
			deleteReq := &route53v2.ChangeResourceRecordSetsInput{
				HostedZoneId: zone.Id,
				ChangeBatch:  &route53v2types.ChangeBatch{Changes: chunk},
			}

			if _, err := svc.ChangeResourceRecordSets(context.TODO(), deleteReq); err != nil {
				logger.Warningf("unable to delete DNS records for zone %v with error: %v", *zone.Id, err)
			}
		}

		if opts.EnableDNSZoneClean && len(toDelete) > 0 {
			deleteHostZoneReq := &route53v2.DeleteHostedZoneInput{
				Id: zone.Id,
			}
			if _, err := svc.DeleteHostedZone(context.TODO(), deleteHostZoneReq); err != nil {
				logger.Warningf("unable to delete DNS zone %v with error: %v", *zone.Id, err)
			}
		}
	}

	return nil
}

func (Route53ResourceRecordSets) ListAll(opts Options) (*Set, error) {
	svc := route53v2.NewFromConfig(*opts.Config, func(opt *route53v2.Options) {
		opt.Region = opts.Region
	})
	set := NewSet(0)

	var rrsErr error
	err := ListHostedZonesPages(svc, &route53v2.ListHostedZonesInput{}, func(zones *route53v2.ListHostedZonesOutput, _ bool) bool {
		for _, z := range zones.HostedZones {
			zone := z
			if !opts.SkipRoute53ManagementCheck && !zoneIsManaged(&zone) {
				continue
			}
			// ListHostedZones returns "/hostedzone/ABCDEF12345678" but other Route53 endpoints expect "ABCDEF12345678"
			zone.Id = aws2.String(strings.TrimPrefix(*zone.Id, "/hostedzone/"))

			inp := &route53v2.ListResourceRecordSetsInput{HostedZoneId: z.Id}
			err := ListResourceRecordSetsPages(svc, inp, func(recordSets *route53v2.ListResourceRecordSetsOutput, _ bool) bool {
				now := time.Now()
				for _, recordSet := range recordSets.ResourceRecordSets {
					arn := route53ResourceRecordSet{
						account: opts.Account,
						region:  opts.Region,
						zone:    &zone,
						obj:     &recordSet,
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

func ListHostedZonesPages(svc *route53v2.Client, input *route53v2.ListHostedZonesInput, pageFunc func(zones *route53v2.ListHostedZonesOutput, _ bool) bool) error {
	paginator := route53v2.NewListHostedZonesPaginator(svc, input)
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

func (rrs Route53ResourceRecordSets) fetchZoneTags(zoneTags map[string]Tags, svc *route53v2.Client) error {
	return incrementalFetchTags(zoneTags, 10, func(ids []*string) error {
		output, err := svc.ListTagsForResources(context.TODO(), &route53v2.ListTagsForResourcesInput{
			ResourceType: route53v2types.TagResourceTypeHostedzone,
			ResourceIds:  aws2.ToStringSlice(ids),
		})
		if err != nil {
			return err
		}
		for _, rts := range output.ResourceTagSets {
			rtype := rts.ResourceType
			if rtype != route53v2types.TagResourceTypeHostedzone {
				return fmt.Errorf("invalid type in ListTagsForResources output: %s", rtype)
			}
			id := *rts.ResourceId
			_, ok := zoneTags[id]
			if !ok {
				return fmt.Errorf("unknown zone id in ListTagsForResources output: %s", id)
			}
			if zoneTags[id] == nil {
				zoneTags[id] = make(Tags, len(rts.Tags))
			}
			for _, tag := range rts.Tags {
				zoneTags[id].Add(tag.Key, tag.Value)
			}
		}
		return nil
	})
}

type route53ResourceRecordSet struct {
	account string
	region  string
	zone    *route53v2types.HostedZone
	obj     *route53v2types.ResourceRecordSet
}

func (r route53ResourceRecordSet) ARN() string {
	return fmt.Sprintf("arn:aws:route53:%s:%s:%s/%s/%s", r.region, r.account, r.obj.Type, *r.zone.Id, *r.obj.Name)
}

func (r route53ResourceRecordSet) ResourceKey() string {
	return r.ARN()
}
