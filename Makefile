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
GOLANGCI_LINT := $(TOOLS_BIN_DIR)/golangci-lint

export GO111MODULE=on

.PHONY: all
all: build

# TODO(ixdy): containerize
.PHONY: build
build:
	go build ./...

.PHONY: test
test: $(GOTESTSUM)
	$(GOTESTSUM) $${ARTIFACTS:+--junitfile="${ARTIFACTS}/junit.xml"} ./...

.PHONY: clean
clean:
	rm -rf $(TOOLS_BIN_DIR)

.PHONY: update-modules
update-modules:
	go mod tidy
	cd $(TOOLS_DIR) && go mod tidy

.PHONY: verify-boilerplate
verify-boilerplate:
	./hack/verify/verify_boilerplate.py --rootdir=$(CURDIR) --boilerplate-dir=$(CURDIR)/hack/verify/boilerplate

.PHONY: verify-lint
# TODO(ixdy): fix legacy errors and remove --new-from-rev
verify-lint: $(GOLANGCI_LINT)
	./hack/tools/bin/golangci-lint run -v --new-from-rev HEAD~

.PHONY: verify-modules
verify-modules:
	./hack/verify/verify_modules.sh

.PHONY: verify
verify: verify-boilerplate verify-lint verify-modules

# Tools
$(GOTESTSUM): $(TOOLS_DIR)/go.mod
	cd $(TOOLS_DIR) && go build -o $(BIN_DIR)/gotestsum gotest.tools/gotestsum

$(GOLANGCI_LINT): $(TOOLS_DIR)/go.mod
	cd $(TOOLS_DIR) && go build -o $(BIN_DIR)/golangci-lint github.com/golangci/golangci-lint/cmd/golangci-lint

# TODO(ixdy): remove Bazel support
.PHONY: update-bazel
update-bazel:
	bazel run //:gazelle -- update-repos -from_file=go.mod -to_macro=repositories.bzl%go_repositories \
	  -prune=true -build_file_generation=on -build_file_proto_mode=disable
	bazel run //:gazelle -- fix

.PHONY: bazel-build
bazel-build:
	bazel build //...

.PHONY: bazel-test
bazel-test:
	bazel test //...
