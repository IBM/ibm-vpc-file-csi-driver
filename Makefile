#
# Copyright 2025 IBM Corp.
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
#

EXE_DRIVER_NAME=ibm-vpc-file-csi-driver
DRIVER_NAME=vpcFileDriver
IMAGE = ${EXE_DRIVER_NAME}
GOPACKAGES=$(shell go list ./... | grep -v /vendor/ | grep -v /cmd | grep -v /tests | grep -v /pkg/ibmcsidriver/ibmcsidriverfakes)
VERSION := latest
GOPATH := $(shell go env GOPATH)

GIT_COMMIT_SHA="$(shell git rev-parse HEAD 2>/dev/null)"
GIT_REMOTE_URL="$(shell git config --get remote.origin.url 2>/dev/null)"
BUILD_DATE="$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")"
OSS_FILES := go.mod Dockerfile

# Jenkins vars. Set to `unknown` if the variable is not yet defined
BUILD_NUMBER?=unknown
GO111MODULE_FLAG?=on
export GO111MODULE=$(GO111MODULE_FLAG)

COLOR_YELLOW=\033[0;33m
COLOR_RESET=\033[0m

.PHONY: all
all: deps lint vet build test buildimage

.PHONY: driver
driver: deps buildimage

.PHONY: deps
LINT_BIN=$(GOPATH)/bin/golangci-lint
deps:
	echo "Installing dependencies ..."
	go mod download
	command -v gotestcover >/dev/null || go install github.com/pierrre/gotestcover@latest

.PHONY: vet
vet:
	@echo "Running go vet..."
	@go vet ./...


.PHONY: lint
lint:
	@echo "Running standalone linters for Go 1.25 ..."

	@command -v errcheck >/dev/null || go install github.com/kisielk/errcheck@latest
	@errcheck ./...

	@command -v revive >/dev/null || go install github.com/mgechev/revive@latest
	@revive -formatter friendly ./...

	@echo "✔ Linting completed (Go 1.25 safe mode)"

# Repository does not contain vendor/modules.txt file so re-build with go mod vendor
.PHONY: build
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go mod vendor
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -mod=vendor -a \
		-ldflags '-X main.vendorVersion='"${DRIVER_NAME}-${GIT_COMMIT_SHA}"' -extldflags "-static"' \
		-o ${GOPATH}/bin/${EXE_DRIVER_NAME} \
		./cmd/

# 'go test -race' requires cgo, set CGO_ENABLED=1
.PHONY: test
test:
	CGO_ENABLED=1 $(GOPATH)/bin/gotestcover -v -race -short -coverprofile=cover.out ${GOPACKAGES}
	go tool cover -html=cover.out -o=cover.html  # Uncomment this line when UT in place.

.PHONY: ut-coverage
ut-coverage: deps test

.PHONY: coverage
coverage:
	go tool cover -html=cover.out -o=cover.html
	@./scripts/calculateCoverage.sh

.PHONY: buildimage
buildimage: build-systemutil
	docker build	\
        --build-arg git_commit_id=${GIT_COMMIT_SHA} \
        --build-arg git_remote_url=${GIT_REMOTE_URL} \
        --build-arg build_date=${BUILD_DATE} \
        --build-arg jenkins_build_number=${BUILD_NUMBER} \
        --build-arg REPO_SOURCE_URL=${REPO_SOURCE_URL} \
        --build-arg BUILD_URL=${BUILD_URL} \
	-t $(IMAGE):$(VERSION)-amd64 -f Dockerfile .

.PHONY: build-systemutil
build-systemutil:
	docker build --build-arg TAG=$(GIT_COMMIT_SHA) --build-arg OS=linux --build-arg ARCH=$(ARCH) -t csi-driver-builder --pull -f Dockerfile.builder .
	docker run --env GHE_TOKEN=${GHE_TOKEN} csi-driver-builder
	docker cp `docker ps -q -n=1`:/go/bin/${EXE_DRIVER_NAME} ./${EXE_DRIVER_NAME}

.PHONY: test-sanity
test-sanity: deps
	SANITY_PARAMS_FILE=./csi_sanity_params.yaml go test -timeout 60s ./tests/sanity -run ^TestSanity$$ -v

.PHONY: clean
clean:
	rm -rf ${EXE_DRIVER_NAME}
	rm -rf $(GOPATH)/bin/${EXE_DRIVER_NAME}
