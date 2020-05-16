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

all: build

# TODO(ixdy): containerize
build:
	go build ./...

test:
	go test ./...

# TODO(ixdy): remove Bazel support
update-bazel:
	bazel run //:gazelle -- update-repos -from_file=go.mod -to_macro=repositories.bzl%go_repositories \
	  -prune=true -build_file_generation=on -build_file_proto_mode=disable
	bazel run //:gazelle -- fix

bazel-build:
	bazel build //...

bazel-test:
	bazel test //...

.PHONY: all build test update-bazel bazel-build bazel-test
