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

# Installs a few extra tools folks might want to use when running boskosctl.

ARG go_version
ARG alpine_version=3.17

FROM golang:${go_version}-alpine${alpine_version} as build
WORKDIR /go/src/app

RUN apk add bash make

# Cache module downloads
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .

ARG DOCKER_TAG
ENV DOCKER_TAG=${DOCKER_TAG}

ARG cmd
RUN make "${cmd}"

FROM alpine:${alpine_version}

RUN apk add bash jq

ARG cmd
COPY --from=build "/go/src/app/_output/bin/${cmd}" /bin/boskosctl

ENTRYPOINT ["/bin/boskosctl"]
