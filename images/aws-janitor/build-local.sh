#!/bin/bash
# Copyright 2024 The Kubernetes Authors.
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

# Simple script to build aws-janitor image locally for testing

set -o errexit
set -o nounset
set -o pipefail

DOCKER_REPO="${DOCKER_REPO:-localhost}"
DOCKER_TAG="${DOCKER_TAG:-$(date -u '+%Y%m%d')-$(git describe --tags --always --dirty 2>/dev/null || echo 'dev')}"
GO_VERSION="${GO_VERSION:-1.23.4}"
CONTAINER_ENGINE="${CONTAINER_ENGINE:-$(which podman || which docker)}"

echo "Building aws-janitor image..."
echo "  Repository: ${DOCKER_REPO}"
echo "  Tag: ${DOCKER_TAG}"
echo "  Go Version: ${GO_VERSION}"
echo "  Engine: ${CONTAINER_ENGINE}"
echo ""

cd "$(git rev-parse --show-toplevel)"

${CONTAINER_ENGINE} build \
  --build-arg "DOCKER_TAG=${DOCKER_TAG}" \
  --build-arg "go_version=${GO_VERSION}" \
  --build-arg "cmd=aws-janitor" \
  -t "${DOCKER_REPO}/aws-janitor:${DOCKER_TAG}" \
  -t "${DOCKER_REPO}/aws-janitor:latest" \
  -f "./images/aws-janitor/Dockerfile" .

echo ""
echo "Successfully built:"
echo "  ${DOCKER_REPO}/aws-janitor:${DOCKER_TAG}"
echo "  ${DOCKER_REPO}/aws-janitor:latest"
echo ""
echo "Test with:"
echo "  ${CONTAINER_ENGINE} run --rm ${DOCKER_REPO}/aws-janitor:latest --help"
