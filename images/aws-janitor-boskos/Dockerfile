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

# The AWS janitor boskos integration requires AWS credentials.

ARG go_version

FROM golang:${go_version} as build
WORKDIR /go/src/app

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

FROM debian:bookworm-slim

# Install ca-certificates for HTTPS connections to Boskos
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

ARG cmd
COPY --from=build "/go/src/app/_output/bin/${cmd}" /bin/aws-janitor-boskos

ENTRYPOINT ["/bin/aws-janitor-boskos"]
