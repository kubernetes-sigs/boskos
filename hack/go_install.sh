#!/usr/bin/env bash
# Copyright 2021 The Kubernetes Authors.
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
set -o pipefail

module=${1}
binary_name=${2}
version=${3}

if [[ -z "${module}" ]]; then
  echo "must provide module as first parameter"
  exit 1
fi

if [[ -z "${binary_name}" ]]; then
  echo "must provide binary name as second parameter"
  exit 1
fi

if [[ -z "${version}" ]]; then
  echo "must provide version as third parameter"
  exit 1
fi

if [[ -z "${GOBIN}" ]]; then
  echo "GOBIN is not set. Must set GOBIN to install the bin in a specified directory."
  exit 1
fi

tmp_dir=$(mktemp -d -t goinstall_XXXXXXXXXX)
function clean {
  rm -rf "${tmp_dir}"
}
trap clean EXIT

rm "${GOBIN}/${binary_name}"* || true

cd "${tmp_dir}"

# create a new module in the tmp directory
go mod init fake/mod

# install the golang module specified as the first argument
go get -tags tools "${module}@${version}"
mv "${GOBIN}/${binary_name}" "${GOBIN}/${binary_name}-${version}"
ln -sf "${GOBIN}/${binary_name}-${version}" "${GOBIN}/${binary_name}"
