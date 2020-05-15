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

TOOLS_DIR := hack/tools
BIN_DIR := bin
TOOLS_BIN_DIR := $(TOOLS_DIR)/$(BIN_DIR)

GOTESTSUM := $(TOOLS_BIN_DIR)/gotestsum

export GO111MODULE=on

all: build

# TODO(ixdy): containerize
build:
	go build ./...

test: $(GOTESTSUM)
	$(GOTESTSUM) $${ARTIFACTS:+--junitfile="${ARTIFACTS}/junit.xml"} ./...

clean:
	rm -rf $(TOOLS_BIN_DIR)

update-modules:
	go mod tidy
	cd $(TOOLS_DIR) && go mod tidy

verify-boilerplate:
	./hack/verify/verify_boilerplate.py --rootdir=$(CURDIR) --boilerplate-dir=$(CURDIR)/hack/verify/boilerplate

verify: verify-boilerplate

# Tools
$(GOTESTSUM): $(TOOLS_DIR)/go.mod
	cd $(TOOLS_DIR) && go build -o $(BIN_DIR)/gotestsum gotest.tools/gotestsum

# TODO(ixdy): remove Bazel support
update-bazel:
	bazel run //:gazelle -- update-repos -from_file=go.mod -to_macro=repositories.bzl%go_repositories \
	  -prune=true -build_file_generation=on -build_file_proto_mode=disable
	bazel run //:gazelle -- fix

bazel-build:
	bazel build //...

bazel-test:
	bazel test //...

.PHONY: all build test update-modules clean verify-boilerplate verify update-bazel bazel-build bazel-test
