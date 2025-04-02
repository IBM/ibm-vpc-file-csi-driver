#
# Copyright 2019 IBM Corp.
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

GIT_COMMIT_SHA="$(shell git rev-parse HEAD 2>/dev/null)"
GIT_REMOTE_URL="$(shell git config --get remote.origin.url 2>/dev/null)"
BUILD_DATE="$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")"
OSS_FILES := go.mod Dockerfile

# Search for Client Arch, adjust for docker and podman differences
# https://github.com/docker/cli/blob/master/cli/command/system/version.go#L32
# https://github.com/containers/podman/blob/main/cmd/podman/system/version.go#L100
# ARCH=$(shell docker version -f {{.Client.Arch}})
ARCH=$(shell docker version -f {{.Client.Arch}} || docker version -f {{.Client.OsArch}} | xargs basename)

# Jenkins vars. Set to `unknown` if the variable is not yet defined
BUILD_NUMBER?=unknown
GO111MODULE_FLAG?=on
export GO111MODULE=$(GO111MODULE_FLAG)

export LINT_VERSION="1.56.2"

COLOR_YELLOW=\033[0;33m
COLOR_RESET=\033[0m

.PHONY: all
all: deps fmt build test buildimage

.PHONY: driver
driver: deps buildimage

.PHONY: deps
LINT_BIN=$(shell go env GOPATH)/bin/golangci-lint
deps:
	@echo "Installing dependencies ..."
	go mod download

	@if ! command -v $(LINT_BIN) >/dev/null || [[ "$$($(LINT_BIN) --version)" != *${LINT_VERSION}* ]]; then \
		curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin v${LINT_VERSION}; \
	fi

.PHONY: print
print:
	@echo "GOPATH: $(shell go env GOPATH)"
	@echo "LINT_BIN: $(LINT_BIN)"

.PHONY: lint
lint:
	$(LINT_BIN) run --timeout 600s

.PHONY: fmt
fmt: lint
	$(LINT_BIN) run --disable-all --enable=gofmt --timeout 600s
	@if [ -n "$$($(LINT_BIN) run)" ]; then echo 'Please run ${COLOR_YELLOW}make dofmt${COLOR_RESET} on your code.' && exit 1; fi

.PHONY: dofmt
dofmt:
	$(LINT_BIN) run --disable-all --enable=gofmt --fix --timeout 600s

# Repository does not contain vendor/modules.txt file so re-build with go mod vendor
.PHONY: build
build:
	CGO_ENABLED=0 GOOS=$(shell go env GOOS) GOARCH=$(shell go env GOARCH) \
		go mod vendor
	CGO_ENABLED=0 GOOS=$(shell go env GOOS) GOARCH=$(shell go env GOARCH) \
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
ut-coverage: deps fmt test

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
	-t $(IMAGE):$(VERSION)-$(ARCH) -f Dockerfile .
ifeq ($(ARCH), amd64)
	docker tag $(IMAGE):$(VERSION)-$(ARCH) $(IMAGE):$(VERSION)
endif

.PHONY: build-systemutil
build-systemutil:
	docker build --build-arg TAG=$(GIT_COMMIT_SHA) --build-arg OS=linux --build-arg ARCH=$(ARCH) -t csi-driver-builder --pull -f Dockerfile.builder .
	docker run --env GHE_TOKEN=${GHE_TOKEN} csi-driver-builder
	docker cp `docker ps -q -n=1`:/go/bin/${EXE_DRIVER_NAME} ./${EXE_DRIVER_NAME}

.PHONY: test-sanity
test-sanity: deps fmt
	SANITY_PARAMS_FILE=./csi_sanity_params.yaml go test -timeout 60s ./tests/sanity -run ^TestSanity$$ -v

.PHONY: clean
clean:
	rm -rf ${EXE_DRIVER_NAME}
	rm -rf $(GOPATH)/bin/${EXE_DRIVER_NAME}

.PHONY: runanalyzedeps
runanalyzedeps:
	@docker build --rm --build-arg ARTIFACTORY_API_KEY="${ARTIFACTORY_API_KEY}"  -t armada/analyze-deps -f Dockerfile.dependencycheck .
	docker run -v `pwd`/dependency-check:/results armada/analyze-deps

.PHONY: analyzedeps
analyzedeps:
	/tmp/dependency-check/bin/dependency-check.sh --enableExperimental --log /results/logfile --out /results --disableAssembly \
		--suppress /src/dependency-check/suppression-file.xml --format JSON --prettyPrint --failOnCVSS 0 --scan /src

.PHONY: showanalyzedeps
showanalyzedeps:
	grep "VULNERABILITY FOUND" dependency-check/logfile;
	cat dependency-check/dependency-check-report.json |jq '.dependencies[] | select(.vulnerabilities | length>0)';

.PHONY: updatebaseline
updatebaseline:
	detect-secrets scan --update .secrets.baseline --all-files --exclude-files go.sum --db2-scan

.PHONY: auditbaseline
auditbaseline:
	detect-secrets audit .secrets.baseline
