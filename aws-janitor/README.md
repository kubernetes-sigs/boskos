# AWS Janitor

AWS Janitor is a tool for cleaning up stale AWS resources. It's designed to help manage ephemeral test environments by automatically deleting resources that have exceeded their time-to-live (TTL).

## Overview

AWS Janitor operates in two modes:

1. **Mark and Sweep** (default): Tracks resources over time using S3-stored state, deleting them after they exceed the configured TTL
2. **Clean All** (`--all`): Immediately lists and deletes all managed resources without tracking state

## Installation

```bash
go build -o aws-janitor ./cmd/aws-janitor
```

Or from the repository root:
```bash
make build
```

## Usage

### Basic Examples

```bash
# Mark and sweep with 24-hour TTL, storing state in S3
aws-janitor \
  --ttl=24h \
  --path=s3://my-bucket/janitor-state.json \
  --region=us-east-1

# Clean all resources immediately (no state tracking)
aws-janitor \
  --all \
  --ttl=0s \
  --region=us-east-1 \
  --dry-run

# Clean resources with specific tags
aws-janitor \
  --ttl=24h \
  --include-tags=temporary,environment=test \
  --exclude-tags=permanent,environment=production \
  --path=s3://my-bucket/janitor-state.json
```

### Tag Filtering

Control which resources are managed using tag-based filters:

#### Include Tags
Resources **must have ALL** of these tags to be managed:

```bash
--include-tags=team=engineering,temporary=true
```

Tag format:
- `key=value` - Matches exact key-value pair
- `key` - Matches any value for that key

#### Exclude Tags
Resources with **ANY** of these tags will **NOT** be managed:

```bash
--exclude-tags=permanent,environment=production,do-not-delete
```

**Note**: Exclude tags take precedence over include tags.

#### Example: Cleanup Test Resources Only

```bash
# Only clean resources tagged as temporary test resources
aws-janitor \
  --ttl=48h \
  --include-tags=environment=test,temporary=true \
  --exclude-tags=permanent \
  --path=s3://cleanup-state/test-env.json \
  --dry-run
```

### Per-Resource TTL Override

Resources can override the global TTL using a tag:

```bash
aws-janitor \
  --ttl=24h \
  --ttl-tag-key=janitor-ttl \
  --path=s3://my-bucket/state.json
```

Then tag resources with `janitor-ttl=48h` to give them a 48-hour TTL instead of the global 24 hours.

**Note**: This only works when the global `--ttl` is not `0s`.

## Command-Line Flags

### Required Flags

| Flag | Description | Example |
|------|-------------|---------|
| `--path` | S3 path for state storage (required unless `--all` is used) | `s3://bucket/state.json` |

### Core Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--ttl` | `24h` | Maximum time before deleting a resource. Set to `0s` to delete immediately |
| `--region` | (all regions) | Specific AWS region to clean. Omit to clean all regions |
| `--all` | `false` | Clean all resources immediately without state tracking |
| `--dry-run` | `false` | Log what would be deleted without actually deleting |
| `--log-level` | `info` | Log level: `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic` |

### Tag Filtering

| Flag | Description | Example |
|------|-------------|---------|
| `--include-tags` | Comma-separated tags that resources must have to be managed | `team=eng,env=test` |
| `--exclude-tags` | Comma-separated tags that prevent resources from being managed | `permanent,env=prod` |
| `--ttl-tag-key` | Tag key for per-resource TTL override | `janitor-ttl` |

### Resource-Specific Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--enable-target-group-clean` | `false` | Enable cleaning of ALB/NLB target groups |
| `--enable-key-pairs-clean` | `false` | Enable cleaning of EC2 key pairs |
| `--enable-vpc-endpoints-clean` | `false` | Enable cleaning of VPC endpoints |
| `--enable-dns-zone-clean` | `false` | Enable deletion of Route53 hosted zones |
| `--enable-s3-buckets-clean` | `false` | Enable cleaning of S3 buckets |
| `--skip-route53-management-check` | `false` | Skip built-in Route53 zone/record filtering |
| `--skip-resource-record-set-types` | `SOA,NS` | Route53 record types to never delete |
| `--clean-ecr-repositories` | (none) | Comma-separated list of ECR repos to clean images from |

### Metrics

| Flag | Description |
|------|-------------|
| `--push-gateway` | Prometheus push gateway endpoint for metrics |

## Resources Managed

### Regional Resources (cleaned by default)

- **CloudFormation Stacks**
- **EKS Clusters**
- **Load Balancers** (Classic ELB, ALB, NLB)
- **Auto Scaling Groups**
- **Launch Configurations**
- **Launch Templates**
- **EC2 Instances** (running and pending)
- **Network Interfaces**
- **Subnets**
- **Security Groups**
- **Internet Gateways**
- **Route Tables**
- **NAT Gateways**
- **VPCs**
- **DHCP Options**
- **EBS Snapshots**
- **EBS Volumes**
- **Elastic IPs**
- **EFS File Systems**
- **SQS Queues**

### Conditionally Cleaned (require flags)

- **Target Groups** (`--enable-target-group-clean`)
- **Key Pairs** (`--enable-key-pairs-clean`)
- **VPC Endpoints** (`--enable-vpc-endpoints-clean`)
- **S3 Buckets** (`--enable-s3-buckets-clean`)
- **ECR Images** (`--clean-ecr-repositories=repo1,repo2`)

### Global Resources (non-regional)

- **IAM Instance Profiles**
- **IAM Roles**
- **IAM OIDC Providers**
- **Route53 Resource Record Sets** (with strict built-in filtering)

## Route53 Special Handling

Route53 has built-in safety checks that cannot be disabled without `--skip-route53-management-check`:

1. **Zone filtering**: Only manages zones named `test-cncf-aws.k8s.io.`
2. **Record filtering**: Only deletes type "A" records matching specific patterns:
   - `api.e2e-*`
   - `api.internal.e2e-*`
   - `main.etcd.e2e-*`
   - `events.etcd.e2e-*`
   - `kops-controller.internal.e2e-*`

To manage other zones/records, use `--skip-route53-management-check` (not recommended for production).

## AWS Credentials

AWS Janitor uses the standard AWS SDK credential chain:

1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
2. Shared credentials file (`~/.aws/credentials`)
3. IAM role (when running on EC2/ECS/Lambda)

### Required IAM Permissions

The AWS credentials must have permissions to:
- List and describe all resource types being cleaned
- Terminate/delete those resources
- S3 read/write access to the state bucket (for mark-and-sweep mode)

## Safety Considerations

### Always Use --dry-run First

```bash
aws-janitor --all --ttl=0s --dry-run --log-level=debug
```

Review the logs to ensure only intended resources will be deleted.

### State Bucket Protection

When using `--enable-s3-buckets-clean`, the S3 bucket storing janitor state **must** be tagged with exclude tags:

```bash
# Tag the state bucket to prevent self-deletion
aws s3api put-bucket-tagging \
  --bucket my-janitor-state \
  --tagging 'TagSet=[{Key=janitor-exclude,Value=true}]'

# Then run janitor with matching exclude tag
aws-janitor \
  --enable-s3-buckets-clean \
  --exclude-tags=janitor-exclude \
  --path=s3://my-janitor-state/state.json
```

### Resource Dependencies

Resources are cleaned in dependency order to avoid deletion failures. For example:
1. EC2 instances are terminated before network interfaces
2. Subnets are deleted before VPCs
3. Route tables are deleted before internet gateways

### Default TTL

The default TTL is 24 hours. Resources created within the TTL window won't be deleted.

Set `--ttl=0s` to delete all managed resources immediately (use with caution).

## Mark and Sweep Mode

Mark and sweep mode tracks resources over time using S3-stored state:

1. **First run**: Discovers all resources and records their creation time
2. **Subsequent runs**: Checks TTL and deletes resources that have exceeded it
3. **State cleanup**: Removes entries for resources that no longer exist

### State File Format

The state file is a JSON map of ARN to first-seen timestamp:

```json
{
  "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0": "2024-03-01T10:00:00Z",
  "arn:aws:ec2:us-east-1:123456789012:volume/vol-0123456789abcdef": "2024-03-02T15:30:00Z"
}
```

### Example Workflow

```bash
# Day 1: Mark resources (nothing deleted yet if TTL > 0)
aws-janitor \
  --ttl=48h \
  --include-tags=temporary=true \
  --path=s3://cleanup-state/prod-account.json

# Day 3: Resources older than 48h are now deleted
aws-janitor \
  --ttl=48h \
  --include-tags=temporary=true \
  --path=s3://cleanup-state/prod-account.json
```

## Clean All Mode

Clean all mode (`--all`) doesn't track state - it lists and deletes resources in a single pass:

```bash
aws-janitor \
  --all \
  --ttl=0s \
  --include-tags=temporary=true \
  --exclude-tags=permanent \
  --region=us-west-2
```

**Use case**: One-time cleanup operations or scheduled cleanup without persistent state.

## Prometheus Metrics

Export metrics to Prometheus Push Gateway:

```bash
aws-janitor \
  --push-gateway=http://prometheus-pushgateway:9091 \
  --ttl=24h \
  --path=s3://state/janitor.json
```

Metrics exported:
- `aws_janitor_job_duration_time_seconds` - Job execution duration
- `aws_janitor_swept_resources` - Number of resources deleted

## Troubleshooting

### No resources are being deleted

1. Check tag filtering - resources might not match `--include-tags` or match `--exclude-tags`
2. Verify TTL - resources might not be old enough
3. Check logs with `--log-level=debug`
4. Use `--dry-run` to see what would be deleted

### Permission errors

Ensure AWS credentials have sufficient IAM permissions to list and delete all resource types.

### State file errors

If the S3 state file is corrupted:
1. Back up the current state file
2. Delete or fix the state file
3. Re-run janitor (it will recreate state from scratch)

## Examples

### Example 1: Clean Test Environment Daily

```bash
#!/bin/bash
# Cron: 0 2 * * * /path/to/cleanup-test.sh

aws-janitor \
  --ttl=72h \
  --include-tags=environment=test \
  --exclude-tags=permanent,environment=production \
  --enable-s3-buckets-clean \
  --enable-key-pairs-clean \
  --path=s3://janitor-state/test-env.json \
  --log-level=info
```

### Example 2: Emergency Cleanup

```bash
# Delete all temporary resources immediately
aws-janitor \
  --all \
  --ttl=0s \
  --include-tags=temporary=true \
  --dry-run \
  --log-level=debug

# After reviewing logs, run without dry-run
aws-janitor \
  --all \
  --ttl=0s \
  --include-tags=temporary=true
```

### Example 3: Multi-Region Cleanup

```bash
# Clean all regions
aws-janitor \
  --ttl=24h \
  --include-tags=team=ci \
  --path=s3://global-janitor-state/ci-resources.json

# Clean specific region only
aws-janitor \
  --ttl=24h \
  --include-tags=team=ci \
  --region=us-east-1 \
  --path=s3://global-janitor-state/ci-us-east-1.json
```

## Contributing

See the main [Boskos README](../README.md) for contribution guidelines.

## License

Apache License 2.0 - see the main repository for details.
