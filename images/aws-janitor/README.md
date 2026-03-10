# AWS Janitor Container Image

This directory contains the Dockerfile for building the AWS Janitor container image.

## Features

- Built on Debian Bookworm Slim for minimal size while maintaining compatibility
- Includes AWS CLI v2 for debugging and potential future enhancements
- Multi-architecture support (amd64, arm64, ppc64le, s390x)

## Building

### Using Make (recommended)

Build the image using the project's standard build system:

```bash
# Set required environment variables
export DOCKER_REPO=gcr.io/your-project
export DOCKER_TAG=v$(date -u '+%Y%m%d')-$(git describe --tags --always --dirty)

# Build the image (requires docker with buildx for multi-arch)
make aws-janitor-image
```

### Local build with Podman/Docker

For local testing with a single architecture:

```bash
# Using podman (single arch)
podman build \
  --build-arg "DOCKER_TAG=test" \
  --build-arg "go_version=1.23.4" \
  --build-arg "cmd=aws-janitor" \
  -t localhost/aws-janitor:test \
  -f ./images/aws-janitor/Dockerfile .

# Using docker (single arch)
docker build \
  --build-arg "DOCKER_TAG=test" \
  --build-arg "go_version=1.23.4" \
  --build-arg "cmd=aws-janitor" \
  -t localhost/aws-janitor:test \
  -f ./images/aws-janitor/Dockerfile .
```

## Usage

### Basic Usage

```bash
docker run -it --rm \
  -e AWS_ACCESS_KEY_ID=your-key \
  -e AWS_SECRET_ACCESS_KEY=your-secret \
  gcr.io/your-project/aws-janitor:latest \
  --dry-run \
  --path s3://your-bucket/janitor-state.json \
  --region us-east-1 \
  --ttl=24h
```

### With AWS Credentials File

```bash
docker run -it --rm \
  -v ~/.aws:/root/.aws:ro \
  gcr.io/your-project/aws-janitor:latest \
  --dry-run \
  --path s3://your-bucket/janitor-state.json \
  --region us-east-1
```

### Kubernetes CronJob Example

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: aws-janitor
spec:
  schedule: "0 */6 * * *"  # Run every 6 hours
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: aws-janitor
          containers:
          - name: aws-janitor
            image: gcr.io/your-project/aws-janitor:latest
            args:
            - --path=s3://cleanup-state/janitor.json
            - --region=us-east-1
            - --ttl=72h
            - --include-tags=temporary=true
            - --exclude-tags=permanent
            env:
            - name: AWS_REGION
              value: us-east-1
            # AWS credentials should be provided via IAM roles for service accounts (IRSA)
            # or by mounting a secret
          restartPolicy: OnFailure
```

## Security Considerations

1. **Preserve Tag**: Resources tagged with `preserve` are automatically excluded from cleanup as a safety mechanism
2. **Credentials**: Use IAM roles when running in AWS (EKS, EC2) rather than static credentials
3. **Dry Run**: Always test with `--dry-run` first
4. **Least Privilege**: Grant only necessary IAM permissions for the resources you want to clean

## Image Size

The image is approximately 200-300MB due to:
- Go binary: ~90MB
- AWS CLI v2: ~100MB
- Debian base: minimal

## Debugging

The AWS CLI is available in the container for debugging:

```bash
docker run -it --rm \
  -v ~/.aws:/root/.aws:ro \
  gcr.io/your-project/aws-janitor:latest \
  /bin/bash -c "aws ec2 describe-instances --region us-east-1"
```
