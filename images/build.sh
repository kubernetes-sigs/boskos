#!/bin/bash
# Copyright 2020 The Kubernetes Authors.
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

# This takes in environment variables and outputs data used by Bazel
# to set key-value pairs

set -o errexit
set -o nounset
set -o pipefail

multi_arch_platforms="linux/amd64,linux/arm64,linux/ppc64le"

if [[ -z "${DOCKER_REPO:-}" ]]; then
    echo "DOCKER_REPO must be set!" >&2
    exit 1
fi

if [[ -z "${DOCKER_TAG:-}" ]]; then
    echo "DOCKER_TAG must be set!" >&2
    exit 1
fi

image_build() {
    local cmd=$1
    local image_dir
    local container_engine=$2
    if [[ -d ./images/"${cmd}" ]]; then
        image_dir="${cmd}"
    else
        image_dir="default"
    fi

    if [[ -z "${container_engine}" ]]; then
        echo "CONTAINER_ENGINE must be set!" >&2
        exit 1
    fi

    # We need to set DOCKER_TAG in the container because git metadata isn't available
    ${container_engine} buildx build --builder multiarch \
        --platform=${multi_arch_platforms} \
        --output type=registry --pull \
        --build-arg "DOCKER_TAG=${DOCKER_TAG}" \
        --build-arg "go_version=${GO_VERSION}" \
        --build-arg "cmd=${cmd}" \
        -t "${DOCKER_REPO}/${cmd}:${DOCKER_TAG}" \
        -t "${DOCKER_REPO}/${cmd}:latest" \
        -f "./images/${image_dir}/Dockerfile" .
}

image_build $@
