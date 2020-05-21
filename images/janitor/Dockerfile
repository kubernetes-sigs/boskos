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

# The GCP janitor requires gcloud, so this image is based on the google cloud-sdk image.

ARG go_version

FROM golang:${go_version} as build
WORKDIR /go/src/app

# Cache module downloads
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .
# Cache build output too
RUN make build

ARG package
RUN make build WHAT="${package}"

FROM gcr.io/google.com/cloudsdktool/cloud-sdk:277.0.0-slim
ARG binary
COPY ./cmd/janitor/gcp_janitor.py /bin
COPY --from=build "/go/src/app/_output/bin/${binary}" /bin/janitor

# the entrypoint must be sh
# https://github.com/kubernetes/test-infra/issues/5877
ENTRYPOINT ["/bin/sh", "-c", "/bin/echo starting janitor && /bin/janitor \"$@\"", "--"]
