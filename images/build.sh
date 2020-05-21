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

if [[ -z "${DOCKER_REPO:-}" ]]; then
    echo "DOCKER_REPO must be set!" >&2
    exit 1
fi

if [[ -z "${DOCKER_TAG:-}" ]]; then
    echo "DOCKER_TAG must be set!" >&2
    exit 1
fi

docker_build() {
    local binary=$1
    local package
    # TODO(ixdy): fix project layout for aws-janitor binaries
    case "${binary}" in
        aws-janitor)
            package="./aws-janitor"
            ;;
        aws-janitor-boskos)
            package="./aws-janitor/cmd/aws-janitor-boskos"
            ;;
        *)
            package="./cmd/${binary}"
    esac
    local image_dir
    if [[ -d ./images/"${binary}" ]]; then
        image_dir="${binary}"
    else
        image_dir="default"
    fi
    docker build --pull \
        --build-arg "go_version=${GO_VERSION}" \
        --build-arg "package=${package}" \
        --build-arg "binary=${binary}" \
        -t "${DOCKER_REPO}/${binary}:${DOCKER_TAG}" \
        -t "${DOCKER_REPO}/${binary}:latest" \
        -f "./images/${image_dir}/Dockerfile" .
}

docker_build $@
