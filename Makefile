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

WHAT ?= ./...
DOCKER_REPO ?= gcr.io/k8s-staging-boskos
DOCKER_TAG ?= v$(shell date -u '+%Y%m%d')-$(shell git describe --tags --always --dirty)
OUTPUT_DIR ?= _output

TOOLS_DIR := hack/tools
BIN_DIR := bin
TOOLS_BIN_DIR := $(TOOLS_DIR)/$(BIN_DIR)
OUTPUT_BIN_DIR := $(OUTPUT_DIR)/$(BIN_DIR)

GOTESTSUM := $(TOOLS_BIN_DIR)/gotestsum
GOLANGCI_LINT := $(TOOLS_BIN_DIR)/golangci-lint

CMDS = $(notdir $(shell find ./cmd/ -maxdepth 1 -type d | sort))

export GO_VERSION=1.15.5
export GO111MODULE=on
export DOCKER_REPO
export DOCKER_TAG

.PHONY: all
all: build

.PHONY: $(CMDS)
$(CMDS):
	MINIMUM_GO_VERSION=go$(GO_VERSION) ./hack/ensure-go.sh
	mkdir -p "$(OUTPUT_BIN_DIR)"
	go build -o "$(OUTPUT_BIN_DIR)" \
		-ldflags " \
			-X 'k8s.io/test-infra/prow/version.Name=$@' \
			-X 'k8s.io/test-infra/prow/version.Version=$(DOCKER_TAG)' \
		" \
		./cmd/$@

# TODO(ixdy): containerize
.PHONY: build
build:
	MINIMUM_GO_VERSION=go$(GO_VERSION) ./hack/ensure-go.sh
	go build $(WHAT)

.PHONY: test
test: $(GOTESTSUM)
	MINIMUM_GO_VERSION=go$(GO_VERSION) ./hack/ensure-go.sh
	$(GOTESTSUM) $${ARTIFACTS:+--junitfile="${ARTIFACTS}/junit.xml"} $(WHAT)

.PHONY: images
images: $(patsubst %,%-image,$(CMDS))

.PHONY: %-image
%-image:
	./images/build.sh $*

.PHONY: clean
clean:
	rm -rf "$(OUTPUT_DIR)"
	rm -rf "$(TOOLS_BIN_DIR)"

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
