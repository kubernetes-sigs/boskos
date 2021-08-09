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

# Most of the images can be based on distroless, which is fairly lightweight.

ARG go_version

FROM golang:${go_version} as build
WORKDIR /go/src/app

#FROM registry.ci.openshift.org/ocp/builder:rhel-8-golang-1.15-openshift-4.8 AS builder

# Cache module downloads
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .

ARG DOCKER_TAG
ENV DOCKER_TAG=${DOCKER_TAG}

# Cache build output too
RUN make build

ARG cmd
RUN make "${cmd}"

FROM fedora:34
RUN dnf install -y libvirt-client nss_wrapper

ARG cmd
COPY --from=build "/go/src/app/_output/bin/${cmd}" /app

#COPY --from=builder /go/src/github.com/openshift/installer/images/libvirt/mock-nss.sh /bin/mock-nss.sh
RUN curl --output /bin/mock-nss.sh https://raw.githubusercontent.com/openshift/installer/master/images/libvirt/mock-nss.sh && chmod ugo+x /bin/mock-nss.sh

ENTRYPOINT ["/app"]
