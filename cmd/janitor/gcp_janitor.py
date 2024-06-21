#!/usr/bin/env python3

# Copyright 2016 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Clean up resources from gcp projects. """

import argparse
import collections
import datetime
import json
import os
import subprocess
import sys
import threading

# A resource that need to be cleared.
# Any names in preserved_names will never be deleted.
Resource = collections.namedtuple(
    'Resource', 'api_version group name subgroup condition managed tolerate bulk_delete preserved_names')
RESOURCES_BY_API = {
    # [WARNING FROM KRZYZACY] : TOUCH THIS WITH CARE!
    # ORDER (INSIDE EACH API BLOCK) REALLY MATTERS HERE!

    # filestore resources
    'file.googleapis.com': [
        Resource('', 'filestore', 'instances', None, 'zone', None, False, False, None),
    ],
    
    #secretmanager resources
    'secretmanager.googleapis.com': [
        Resource('', 'secretmanager', 'secrets', None, 'zone', None, False, True, None),
    ],
    
    # compute resources
    'compute.googleapis.com': [
        Resource('', 'compute', 'instances', None, 'zone', None, False, True, None),
        Resource('', 'compute', 'addresses', None, 'global', None, False, True, None),
        Resource('', 'compute', 'addresses', None, 'region', None, False, True, None),
        Resource('', 'compute', 'disks', None, 'zone', None, False, True, None),
        Resource('', 'compute', 'disks', None, 'region', None, False, True, None),
        Resource('', 'compute', 'firewall-rules', None, None, None, False, True, None),
        Resource('', 'compute', 'forwarding-rules', None, 'global', None, False, True, None),
        Resource('', 'compute', 'forwarding-rules', None, 'region', None, False, True, None),
        Resource('', 'compute', 'target-http-proxies', None, 'global', None, False, True, None),
        Resource('', 'compute', 'target-http-proxies', None, 'region', None, False, True, None),
        Resource('', 'compute', 'target-https-proxies', None, 'global', None, False, True, None),
        Resource('', 'compute', 'target-https-proxies', None, 'region', None, False, True, None),
        Resource('', 'compute', 'target-tcp-proxies', None, None, None, False, True, None),
        Resource('', 'compute', 'ssl-policies', None, 'global', None, False, True, None),
        Resource('', 'compute', 'ssl-policies', None, 'region', None, False, True, None),
        Resource('', 'compute', 'ssl-certificates', None, 'global', None, False, True, None),
        Resource('', 'compute', 'ssl-certificates', None, 'region', None, False, True, None),
        Resource('', 'compute', 'url-maps', None, 'global', None, False, True, None),
        Resource('', 'compute', 'url-maps', None, 'region', None, False, True, None),
        Resource('', 'compute', 'backend-services', None, 'global', None, False, True, None),
        Resource('', 'compute', 'backend-services', None, 'region', None, False, True, None),
        Resource('', 'compute', 'target-pools', None, 'region', None, False, True, None),
        Resource('', 'compute', 'security-policies', None, 'global', None, False, True, None),
        Resource('', 'compute', 'security-policies', None, 'region', None, False, True, None),
        Resource('', 'compute', 'health-checks', None, 'global', None, False, True, None),
        Resource('', 'compute', 'health-checks', None, 'region', None, False, True, None),
        Resource('', 'compute', 'http-health-checks', None, None, None, False, True, None),
        Resource('', 'compute', 'instance-groups', None, 'region', 'Yes', False, True, None),
        Resource('', 'compute', 'instance-groups', None, 'zone', 'Yes', False, True, None),
        Resource('', 'compute', 'instance-groups', None, 'zone', 'No', False, True, None),
        Resource('', 'compute', 'instance-templates', None, 'global', None, False, True, None),
        Resource('', 'compute', 'instance-templates', None, 'region', None, False, True, None),
        Resource('', 'compute', 'sole-tenancy', 'node-groups', 'zone', None, False, True, None),
        Resource('', 'compute', 'sole-tenancy', 'node-templates', 'region', None, False, True, None),
        Resource('', 'compute', 'network-endpoint-groups', None, 'zone', None, False, False, None),
        Resource('', 'compute', 'routes', None, None, None, False, True, None),
        Resource('', 'compute', 'routers', None, 'region', None, False, True, None),
        Resource('', 'compute', 'networks', 'subnets', 'region', None, True, True, None),
        Resource('', 'compute', 'networks', None, None, None, False, True, None),
    ],

    # logging resources
    'logging.googleapis.com': [
        Resource('', 'logging', 'sinks', None, None, None, False, False, ['_Default', '_Required']),
    ],

    # pubsub resources
    'pubsub.googleapis.com': [
        Resource('', 'pubsub', 'subscriptions', None, None, None, False, True, None),
        Resource('', 'pubsub', 'topics', None, None, None, False, True,
                 ['container-analysis-notes-v1', 'container-analysis-notes-v1beta1',
                  'container-analysis-occurrences-v1', 'container-analysis-occurrences-v1beta1'])
    ],

    # GKE hub memberships
    'gkehub.googleapis.com': [
        Resource('', 'container', 'hub', 'memberships', None, None, False, False, None),
    ],

    # staging GKE hub memberships
    'staging-gkehub.sandbox.googleapis.com': [
        Resource('', 'container', 'hub', 'memberships', None, None, False, False, None),
    ],

    # autopush GKE hub memberships
    'autopush-gkehub.sandbox.googleapis.com': [
        Resource('', 'container', 'hub', 'memberships', None, None, False, False, None),
    ]
}

# gcloud compute zones list --format="value(name)" | sort | awk '{print "    \x27"$1"\x27," }'
BASE_ZONES = [
    'asia-east1-a',
    'asia-east1-b',
    'asia-east1-c',
    'asia-east2-a',
    'asia-east2-b',
    'asia-east2-c',
    'asia-northeast1-a',
    'asia-northeast1-b',
    'asia-northeast1-c',
    'asia-northeast2-a',
    'asia-northeast2-b',
    'asia-northeast2-c',
    'asia-northeast3-a',
    'asia-northeast3-b',
    'asia-northeast3-c',
    'asia-south1-a',
    'asia-south1-b',
    'asia-south1-c',
    'asia-south2-a',
    'asia-south2-b',
    'asia-south2-c',
    'asia-southeast1-a',
    'asia-southeast1-b',
    'asia-southeast1-c',
    'asia-southeast2-a',
    'asia-southeast2-b',
    'asia-southeast2-c',
    'australia-southeast1-a',
    'australia-southeast1-b',
    'australia-southeast1-c',
    'australia-southeast2-a',
    'australia-southeast2-b',
    'australia-southeast2-c',
    'europe-central2-a',
    'europe-central2-b',
    'europe-central2-c',
    'europe-north1-a',
    'europe-north1-b',
    'europe-north1-c',
    'europe-west1-b',
    'europe-west1-c',
    'europe-west1-d',
    'europe-west2-a',
    'europe-west2-b',
    'europe-west2-c',
    'europe-west3-a',
    'europe-west3-b',
    'europe-west3-c',
    'europe-west4-a',
    'europe-west4-b',
    'europe-west4-c',
    'europe-west6-a',
    'europe-west6-b',
    'europe-west6-c',
    'northamerica-northeast1-a',
    'northamerica-northeast1-b',
    'northamerica-northeast1-c',
    'northamerica-northeast2-a',
    'northamerica-northeast2-b',
    'northamerica-northeast2-c',
    'southamerica-east1-a',
    'southamerica-east1-b',
    'southamerica-east1-c',
    'us-central1-a',
    'us-central1-b',
    'us-central1-c',
    'us-central1-f',
    'us-central2-a',
    'us-central2-b',
    'us-central2-c',
    'us-central2-d',
    'us-east1-b',
    'us-east1-c',
    'us-east1-d',
    'us-east4-a',
    'us-east4-b',
    'us-east4-c',
    'us-west1-a',
    'us-west1-b',
    'us-west1-c',
    'us-west2-a',
    'us-west2-b',
    'us-west2-c',
    'us-west3-a',
    'us-west3-b',
    'us-west3-c',
    'us-west4-a',
    'us-west4-b',
    'us-west4-c',
]

def log(message):
    """ print a message if --verbose is set. """
    if ARGS.verbose:
        print('[%s] %s' % (str(datetime.datetime.now()), message))


def base_command(resource):
    """ Return the base gcloud command with api_version, group and subgroup.

    Args:
        resource: Definition of a type of gcloud resource.
    Returns:
        list of base commands of gcloud .
    """

    base = ['gcloud']
    if resource.api_version:
        base += [resource.api_version]
    base += [resource.group, '-q', resource.name]
    if resource.subgroup:
        base.append(resource.subgroup)
    return base


def validate_item(item, age, resource, clear_all):
    """ Validate if an item need to be cleaned.

    Args:
        item: a gcloud resource item from json format.
        age: Time cutoff from the creation of a resource.
        resource: Definition of a type of gcloud resource.
        clear_all: If need to clean regardless of timestamp.
    Returns:
        True if object need to be cleaned, False otherwise.
    Raises:
        ValueError if json result from gcloud is invalid.
    """

    if resource.preserved_names and item['name'] in resource.preserved_names:
        return False

    if resource.managed:
        if 'isManaged' not in item:
            raise ValueError(resource.name, resource.managed)
        if resource.managed != item['isManaged']:
            return False

    # clears everything without checking creationTimestamp
    if clear_all:
        return True

    creationTimestamp = item.get('creationTimestamp', item.get('createTime'))
    if creationTimestamp is None:
        raise ValueError('missing key: creationTimestamp or createTime - %r' % item)

    # Unify datetime to use utc timezone.
    created = datetime.datetime.strptime(creationTimestamp, '%Y-%m-%dT%H:%M:%S')
    log('Found %r(%r), %r, created time = %r' %
        (resource.name, resource.subgroup, item['name'], creationTimestamp))
    if created < age:
        log('Added to janitor list: %r(%r), %r' %
            (resource.name, resource.subgroup, item['name']))
        return True
    return False


def collect(project, zones, age, resource, filt, clear_all):
    """ Collect a list of resources for each condition (zone or region).

    Args:
        project: The name of a gcp project.
        zones: a list of gcp zones to clean up the resources.
        age: Time cutoff from the creation of a resource.
        resource: Definition of a type of gcloud resource.
        filt: Filter clause for gcloud list command.
        clear_all: If need to clean regardless of timestamp.
    Returns:
        A dict of condition : list of gcloud resource object.
    Raises:
        ValueError if json result from gcloud is invalid.
        subprocess.CalledProcessError if cannot list the gcloud resource
    """

    col = collections.defaultdict(list)

    # TODO(krzyzacy): logging sink does not have timestamp
    #                 don't even bother listing it if not clear_all
    if resource.name == 'sinks' and not clear_all:
        return col

    cmd = base_command(resource)
    cmd.extend([
        'list',
        '--format=json(name,creationTimestamp.date(tz=UTC),createTime.date(tz=UTC),zone,region,isManaged)',
        '--project=%s' % project])
    if (resource.condition == 'zone'
            and resource.name != 'sole-tenancy'
            and resource.name != 'network-endpoint-groups'
            and resource.group != 'filestore'):
        cmd.append('--filter=%s AND zone:( %s )' % (filt, ' '.join(zones)))
    else:
        cmd.append('--filter=%s' % filt)
    log('%r' % cmd)

    # TODO(krzyzacy): work around for alpha API list calls
    try:
        items = subprocess.check_output(cmd)
    except subprocess.CalledProcessError:
        if resource.tolerate:
            return col
        raise

    for item in json.loads(items):
        log('parsing item: %r' % item)

        if 'name' not in item:
            raise ValueError('missing key: name - %r' % item)

        colname = ''
        if resource.condition is not None:
            # This subcommand will want either a --global, --region, or --zone
            # flag, so segment items accordingly.
            if resource.condition == 'global':
                if 'zone' in item or 'region' in item:
                    # This item is zonal or regional, so don't include it in
                    # the global list.
                    continue
            elif resource.condition in item:
                # Looking for zonal or regional items, and this matches.
                # The zone or region is sometimes a full URL (why?), but
                # subcommands want just the name, not the full URL, so strip it.
                colname = item[resource.condition].rsplit('/', 1)[-1]
                log('looking for items in %s=%s' % (resource.condition, colname))
            elif resource.group == 'filestore':
                # Filestore instances don't have 'zone' field, but require
                # --zone flag for deletion.
                colname = item['name'].split('/')[3]
            else:
                # This item doesn't match the condition, so don't include it.
                continue

        if validate_item(item, age, resource, clear_all):
            col[colname].append(item['name'])
    return col

def asyncCall(cmd, tolerate, name, errs, lock, hide_output):
    log('%sCall %r' % ('[DRYRUN] ' if ARGS.dryrun else '', cmd))
    if ARGS.dryrun:
        return
    try:
        if hide_output:
            FNULL = open(os.devnull, 'w')
            subprocess.check_call(cmd, stdout=FNULL)
        else:
            subprocess.check_call(cmd)
    except subprocess.CalledProcessError as exc:
        if not tolerate:
            with lock:
                errs.append(exc)
        print('Error try to delete resources %s: %r' % (name, exc),
              file=sys.stderr)

def clear_resources(project, cols, resource, rate_limit):
    """Clear a collection of resource, from collect func above.

    Args:
        project: The name of a gcp project.
        cols: A dict of collection of resource.
        resource: Definition of a type of gcloud resource.
        rate_limit: how many resources to delete per gcloud delete call
    Returns:
        0 if no error
        > 0 if deletion command fails
    """
    errs = []
    threads = list()
    lock = threading.Lock()

    # delete one resource at a time, if there's no api support
    # aka, logging sinks for example
    if not resource.bulk_delete:
        rate_limit = 1

    for col, items in cols.items():
        manage_key = {'Yes': 'managed', 'No': 'unmanaged'}

        # construct the customized gcloud command
        base = base_command(resource)
        if resource.managed:
            base.append(manage_key[resource.managed])
        base.append('delete')
        base.append('--project=%s' % project)

        condition = None
        if resource.condition and col:
            condition = '--%s=%s' % (resource.condition, col)
        elif resource.condition == 'global':
            condition = '--global'

        log('going to delete %d %s' % (len(items), resource.name))
        # try to delete at most $rate_limit items at a time
        for idx in range(0, len(items), rate_limit):
            clean = items[idx:idx + rate_limit]
            cmd = base + list(clean)
            if condition:
                cmd.append(condition)
            if resource.group == 'filestore':
                cmd.append("--force")
            thread = threading.Thread(
                target=asyncCall, args=(cmd, resource.tolerate, resource.name, errs, lock, False))
            threads.append(thread)
            log('start a new thread, total %d' % len(threads))
            thread.start()

    log('Waiting for all %d thread to finish' % len(threads))
    for thread in threads:
        thread.join()
    return len(errs)


def clean_secondary_ip_ranges(project, age, filt):
    """Clean up additional subnet ranges"""

    # List Subnets
    log("Listing subnets")
    cmd = [
        'gcloud', 'compute', 'networks', 'subnets', 'list',
        '--project=%s' % project,
        '--filter=%s' % filt,
        '--format=json(name,region)'
    ]
    log('running %s' % cmd)

    output = ''
    try:
        output = subprocess.check_output(cmd)
    except subprocess.CalledProcessError as exc:
        # expected error
        log('Cannot list subnets with %r, continue' %  (exc))
        raise
    # Get Subnets (Regional)
    subnets = []
    Subnet = collections.namedtuple('Subnet', ['name', 'region'])
    for item in json.loads(output):
        log('subnet info: %r' % item)
        if 'name' not in item:
            raise ValueError('name must be present: %r' % item)
        if 'region' not in item:
            raise ValueError('region must be present: %r' % item)
        name = item['name']
        region = item['region'].split('/')[-1]
        if not region or not name:
            raise ValueError('name and regsion unset')
        subnets.append(Subnet(name, region))

    # List secondary address rangeds
    for subnet in subnets:
        log('Describing subnets')
        cmd = [
            'gcloud', 'compute', 'networks', 'subnets', 'describe',
            '--project=%s' % project,
            '--region=%s' % subnet.region,
            '--format=json(secondaryIpRanges)',
            subnet.name,
        ]
        log('running %s' % cmd)
        output = ''
        try:
            output = subprocess.check_output(cmd)
        except subprocess.CalledProcessError as exc:
            # expected error
            log('Cannot describe subnets with %r, continue' %  (exc))
            continue

        ip_ranges = json.loads(output)
        if not ip_ranges or 'secondaryIpRanges' not in ip_ranges:
            continue
        ranges = []
        for ip_range in ip_ranges['secondaryIpRanges']:
            if 'rangeName' not in ip_range:
                raise ValueError('rangeName not in %s' % ip_range)
            ranges.append(ip_range["rangeName"])

        # Delete secondary ip ranges.
        cmd = [
            'gcloud', 'compute', 'networks', 'subnets', 'update',
            '--project=%s' % project,
            '--region=%s' % subnet.region,
            '--remove-secondary-ranges=%s' % (",".join(ranges)),
            subnet.name,
        ]
        try:
            subprocess.check_output(cmd)
        except subprocess.CalledProcessError as exc:
            # expected error
            log('Cannot delete secondary ip ranges with %r, continue' %  (exc))
            continue


def clean_gke_cluster(project, age, filt):
    """Clean up potential leaking gke cluster"""

    # a cluster can be created in one of those three endpoints
    endpoints = [
        'https://test-container.sandbox.googleapis.com/',  # test
        'https://staging-container.sandbox.googleapis.com/',  # staging
        'https://staging2-container.sandbox.googleapis.com/', # staging2
        'https://container.googleapis.com/',  # prod
    ]

    errs = []

    for endpoint in endpoints:
        threads = list()
        lock = threading.Lock()

        os.environ['CLOUDSDK_API_ENDPOINT_OVERRIDES_CONTAINER'] = endpoint
        log("checking endpoint %s" % endpoint)
        cmd = [
            'gcloud', 'container', '-q', 'clusters', 'list',
            '--project=%s' % project,
            '--filter=%s' % filt,
            '--format=json(name,createTime,region,zone)'
        ]
        log('running %s' % cmd)

        output = ''
        try:
            output = subprocess.check_output(cmd)
        except subprocess.CalledProcessError as exc:
            # expected error
            log('Cannot reach endpoint %s with %r, continue' % (endpoint, exc))
            continue

        for item in json.loads(output):
            log('cluster info: %r' % item)
            if 'name' not in item or 'createTime' not in item:
                raise ValueError('name and createTime must be present: %r' % item)
            if not ('zone' in item or 'region' in item):
                raise ValueError('either zone or region must be present: %r' % item)

            # The raw createTime string looks like 2017-08-30T18:33:14+00:00
            # Which python 2.7 does not support timezones.
            # Since age is already in UTC time we'll just strip the timezone part
            item['createTime'] = item['createTime'].split('+')[0]
            created = datetime.datetime.strptime(
                item['createTime'], '%Y-%m-%dT%H:%M:%S')

            if created < age:
                log('Found stale gke cluster %r in %r, created time = %r' %
                    (item['name'], endpoint, item['createTime']))
                delete = [
                    'gcloud', 'container', '-q', 'clusters', 'delete',
                    item['name'],
                    '--project=%s' % project,
                ]
                if 'zone' in item:
                    delete.append('--zone=%s' % item['zone'])
                elif 'region' in item:
                    delete.append('--region=%s' % item['region'])
                thread = threading.Thread(
                    target=asyncCall, args=(delete, False, item['name'], errs, lock, True))
                threads.append(thread)
                log('start a new thread, total %d' % len(threads))
                thread.start()

        log('Waiting for all %d thread to finish in %s' % (len(threads), endpoint))
        for thread in threads:
            thread.join()

    return len(errs) > 0


def activate_service_account(service_account):
    print('[=== Activating service_account %s ===]' % service_account)
    cmd = [
        'gcloud', 'auth', 'activate-service-account',
        '--key-file=%s' % service_account,
    ]
    log('running %s' % cmd)

    try:
        subprocess.check_call(cmd)
    except subprocess.CalledProcessError:
        print('Error try to activate service_account: %s' % service_account,
              file=sys.stderr)
        return 1
    return 0

def set_quota_project(project):
    print('[=== Setting quota_project %s ===]' % project)
    os.environ['CLOUDSDK_BILLING_QUOTA_PROJECT'] = '%s' % project
    return 0

# Returns whether the specified GCP API is enabled on the provided project.
def api_enabled(project, api):
    log("checking whether API %s is enabled" % api)
    cmd = [
        'gcloud', 'services', '-q', 'list',
        '--project=%s' % project,
        '--filter=config.name="%s"' % api,
        '--format=value(state)'
    ]
    log('running %s' % cmd)
    state = subprocess.check_output(cmd).decode().strip()
    if state == '':
        log('API %s is not enabled' % api)
        return False
    if state == 'ENABLED':
        log('API %s is enabled' % api)
        return True
    print('Unexpected state for API %s: %s' % (api, state))
    return False


def main(project, days, hours, filt, rate_limit, service_account, additional_zones, set_as_quota_project):
    """ Clean up resources from a gcp project based on it's creation time

    Args:
        project: The name of a gcp project.
        days/hours: days/hours of maximum lifetime of a gcp resource.
        filt: Resource instance filters when query.
    Returns:
        0 if no error
        1 if list or delete command fails
    """

    print('[=== Start Janitor on project %r ===]' % project)
    err = 0
    age = datetime.datetime.utcnow() - datetime.timedelta(days=days, hours=hours)
    clear_all = (days == 0 and hours == 0)

    if service_account:
        err |= activate_service_account(service_account)
        if err:
            print('Failed to activate service account %r' % service_account, file=sys.stderr)
            sys.exit(err)

    if set_as_quota_project:
        err |= set_quota_project(project)
        if err:
            print('Failed to set quota project %r' % project, file=sys.stderr)
            sys.exit(err)

    # try to clean leaked secondary ip ranges.
    try:
        clean_secondary_ip_ranges(project, age, filt)
    except ValueError:
        print('Fail to clean up secondary ip ranges from project %r' % project, file=sys.stderr)
 
    # try to clean a leaked GKE cluster first, rather than attempting to delete
    # its associated resources individually.
    try:
        err |= clean_gke_cluster(project, age, filt)
    except ValueError:
        err |= 1  # keep cleaning the other resource
        print('Fail to clean up cluster from project %r' % project, file=sys.stderr)

    zones = BASE_ZONES + additional_zones
    gkehub_apis = {'gkehub.googleapis.com', 'staging-gkehub.sandbox.googleapis.com', 'autopush-gkehub.sandbox.googleapis.com'}
    for api, resources in RESOURCES_BY_API.items():
        if not api_enabled(project, api):
            continue
        if api in gkehub_apis:
            os.environ['CLOUDSDK_API_ENDPOINT_OVERRIDES_GKEHUB'] = 'https://{}/'.format(api)
        for res in resources:
            log('Try to search for %r with condition %r, managed %r' % (
                res.name, res.condition, res.managed))
            try:
                col = collect(project, zones, age, res, filt, clear_all)
                if col:
                    err |= clear_resources(project, col, res, rate_limit)
            except (subprocess.CalledProcessError, ValueError) as exc:
                err |= 1  # keep clean the other resource
                print('Fail to list resource %r from project %r: %r' % (res.name, project, exc),
                      file=sys.stderr)
    print('[=== Finish Janitor on project %r with status %r ===]' % (project, err))
    sys.exit(err)


if __name__ == '__main__':
    PARSER = argparse.ArgumentParser(
        description='Clean up resources from an expired project')
    PARSER.add_argument('--project', help='Project to clean', required=True)
    PARSER.add_argument(
        '--days', type=int,
        help='Clean items more than --days old (added to --hours)')
    PARSER.add_argument(
        '--hours', type=float,
        help='Clean items more than --hours old (added to --days)')
    PARSER.add_argument(
        '--filter',
        default='name !~ ^default',
        help='Filter down to these instances')
    PARSER.add_argument(
        '--dryrun',
        default=False,
        action='store_true',
        help='List but not delete resources')
    PARSER.add_argument(
        '--ratelimit', type=int, default=50,
        help='Max number of resources to bulk clear in one gcloud delete call')
    PARSER.add_argument(
        '--verbose', action='store_true',
        help='Get full janitor output log')
    PARSER.add_argument(
        '--service_account',
        help='GCP service account',
        default=os.environ.get("GOOGLE_APPLICATION_CREDENTIALS", None))
    PARSER.add_argument(
        '--additional_zones',
        nargs="*",
        help='Addtional GCP zones to clean up the GCP resources',
        default=[])
    PARSER.add_argument(
        '--set_as_quota_project',
        default=False,
        action='store_true',
        help='Set the to-be-cleaned project as the quota project')
    ARGS = PARSER.parse_args()

    # We want to allow --days=0 and --hours=0, so check against None instead.
    if ARGS.days is None and ARGS.hours is None:
        print('must specify --days and/or --hours', file=sys.stderr)
        sys.exit(1)

    main(ARGS.project, ARGS.days or 0, ARGS.hours or 0, ARGS.filter,
         ARGS.ratelimit, ARGS.service_account, ARGS.additional_zones, ARGS.set_as_quota_project)
