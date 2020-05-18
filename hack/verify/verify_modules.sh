#!/usr/bin/env bash
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

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
TMP_REPO="$(mktemp --tmpdir -d sigs.k8s.io.boskos.verify-modules.XXXXXX)"

cleanup() {
    if [[ -n "${TMP_REPO:-}" ]]; then
        rm -rf "${TMP_REPO}"
        TMP_REPO=""
    fi
}

trap cleanup EXIT

cp -a "${REPO_ROOT}" "${TMP_REPO}"
cd "${TMP_REPO}/boskos"
make update-modules >/dev/null

if ! git diff --quiet HEAD -- go.sum go.mod hack/tools/go.mod hack/tools/go.sum; then
    echo "!!! Go module files are out of date; run 'make update-modules'"
    exit 1
fi