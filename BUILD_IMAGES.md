# Building AWS Janitor Container Images

This document describes how to build container images for aws-janitor and aws-janitor-boskos.

## Prerequisites

- Docker or Podman installed
- Docker buildx plugin (for multi-arch builds with Docker)
- Access to a container registry (for pushing images)

## Image Structure

We've created two container images:

1. **aws-janitor** - Standalone janitor with AWS CLI
   - Location: `images/aws-janitor/`
   - Based on Debian Bookworm Slim
   - Includes AWS CLI v2 for debugging
   - Size: ~200-300MB

2. **aws-janitor-boskos** - Boskos integration
   - Location: `images/aws-janitor-boskos/`
   - Based on Debian Bookworm Slim
   - Minimal size (~150MB)

## Building Images

### Using Make (Multi-Architecture)

The standard build process creates multi-architecture images (amd64, arm64, ppc64le, s390x):

```bash
# Set your container registry
export DOCKER_REPO=gcr.io/your-project
export DOCKER_TAG=v$(date -u '+%Y%m%d')-$(git describe --tags --always --dirty)

# Build aws-janitor image
make aws-janitor-image

# Build aws-janitor-boskos image
make aws-janitor-boskos-image

# Build all images
make images
```

**Note**: Multi-arch builds require Docker with buildx. Podman is not supported for this.

### Local Build (Single Architecture)

For local testing with Podman or Docker:

```bash
# Using the helper script
cd images/aws-janitor
./build-local.sh

# Or manually
podman build \
  --build-arg "DOCKER_TAG=test" \
  --build-arg "go_version=1.23.4" \
  --build-arg "cmd=aws-janitor" \
  -t localhost/aws-janitor:test \
  -f ./images/aws-janitor/Dockerfile .
```

## Testing Images

### Quick Test

```bash
# Test aws-janitor
podman run --rm localhost/aws-janitor:test --help

# Verify version
podman run --rm localhost/aws-janitor:test --version 2>&1 | head -5

# Test AWS CLI is available
podman run --rm localhost/aws-janitor:test /bin/bash -c "aws --version"
```

### Functional Test

```bash
# Dry-run with actual AWS credentials
podman run --rm \
  -v ~/.aws:/root/.aws:ro \
  localhost/aws-janitor:test \
  --dry-run \
  --path s3://your-bucket/janitor-state.json \
  --region us-east-1 \
  --ttl=24h
```

## Image Updates

The build process automatically:
- ✅ Excludes resources tagged with `preserve` (safety feature)
- ✅ Builds multi-architecture images
- ✅ Includes AWS CLI v2
- ✅ Uses distroless base for smaller size where possible

## Directory Structure

```
images/
├── aws-janitor/
│   ├── Dockerfile          # aws-janitor image definition
│   ├── OWNERS              # Ownership information
│   ├── README.md           # Usage documentation
│   └── build-local.sh      # Local build helper script
├── aws-janitor-boskos/
│   ├── Dockerfile          # aws-janitor-boskos image definition
│   └── OWNERS              # Ownership information
├── build.sh                # Main build script (used by Makefile)
└── default/
    └── Dockerfile          # Fallback for commands without custom images
```

## Troubleshooting

### Build fails with "buildx not found"

Multi-arch builds require Docker buildx. Use local build instead or install buildx.

### Podman build fails with buildx error

Podman doesn't support buildx. Use the local build script or switch to Docker.

### Image size is too large

The aws-janitor image includes AWS CLI v2 which adds ~100MB. For a minimal image, consider:
1. Using the aws-janitor-boskos image (no AWS CLI)
2. Creating a custom Dockerfile based on distroless
3. Building only for your target architecture

### Can't push to registry

Ensure you're logged in to your container registry:

```bash
# For GCR
gcloud auth configure-docker

# For Docker Hub
docker login

# For Podman
podman login gcr.io
```

## CI/CD Integration

The images can be built automatically in CI/CD:

```yaml
# Example GitHub Actions
- name: Build images
  run: |
    export DOCKER_REPO=gcr.io/${{ secrets.GCP_PROJECT }}
    export DOCKER_TAG=${{ github.sha }}
    make aws-janitor-image
    make aws-janitor-boskos-image
```

## See Also

- [aws-janitor/README.md](../aws-janitor/README.md) - AWS Janitor overview
- [images/aws-janitor/README.md](images/aws-janitor/README.md) - Image usage guide
- [Makefile](Makefile) - Build targets
