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


SHELL := /bin/bash

WHAT ?= ./...
DOCKER_REPO ?= gcr.io/k8s-staging-boskos
DOCKER_TAG ?= v$(shell date -u '+%Y%m%d')-$(shell git describe --tags --always --dirty)
OUTPUT_DIR ?= _output

TOOLS_DIR := hack/tools
BIN_DIR := bin
TOOLS_BIN_DIR := $(abspath $(TOOLS_DIR)/$(BIN_DIR))
OUTPUT_BIN_DIR := $(OUTPUT_DIR)/$(BIN_DIR)
GO_INSTALL = ./hack/go_install.sh

GOTESTSUM_VER := v1.8.1
GOTESTSUM_BIN := gotestsum
GOTESTSUM := $(TOOLS_BIN_DIR)/$(GOTESTSUM_BIN)-$(GOTESTSUM_VER)

GOLANGCI_LINT_VER := v1.63.3
GOLANGCI_LINT_BIN := golangci-lint
GOLANGCI_LINT := $(TOOLS_BIN_DIR)/$(GOLANGCI_LINT_BIN)-$(GOLANGCI_LINT_VER)

CONTROLLER_GEN_VER := v0.14.0
CONTROLLER_GEN_BIN := controller-gen
CONTROLLER_GEN := $(TOOLS_BIN_DIR)/$(CONTROLLER_GEN_BIN)-$(CONTROLLER_GEN_VER)

CMDS = $(notdir $(shell find ./cmd/ -maxdepth 1 -type d | sort))

export GO_VERSION=1.23.4
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

.PHONY: verify-boilerplate
verify-boilerplate:
	./hack/verify/verify_boilerplate.py --rootdir=$(CURDIR) --boilerplate-dir=$(CURDIR)/hack/verify/boilerplate

.PHONY: verify-lint
# TODO(ixdy): fix legacy errors and remove --new-from-rev
verify-lint: $(GOLANGCI_LINT)
	./hack/tools/bin/golangci-lint run --timeout='3m0s' --max-issues-per-linter 0 --max-same-issues 0 -v --new-from-rev HEAD~

.PHONY: verify-codegen
verify-codegen: $(CONTROLLER_GEN)
	@make codegen
	@[[ -z $$(git status --porcelain) ]]

.PHONY: codegen
codegen: $(CONTROLLER_GEN)
	./hack/tools/bin/controller-gen object:headerFile=hack/verify/boilerplate/boilerplate.go.txt,year=2021 paths=./crds


.PHONY: verify-modules
verify-modules:
	./hack/verify/verify_modules.sh

.PHONY: verify
verify: verify-boilerplate verify-lint verify-modules verify-codegen

# Tools
$(GOTESTSUM):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) gotest.tools/gotestsum $(GOTESTSUM_BIN) $(GOTESTSUM_VER)

$(GOLANGCI_LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(TOOLS_BIN_DIR) $(GOLANGCI_LINT_VER)

$(CONTROLLER_GEN):
	GOBIN=$(TOOLS_BIN_DIR) $(GO_INSTALL) sigs.k8s.io/controller-tools/cmd/controller-gen $(CONTROLLER_GEN_BIN) $(CONTROLLER_GEN_VER)
