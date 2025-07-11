#!/bin/bash
#/*
# Copyright 2025 The Kubernetes Authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#    http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
# */
set -e
set +x
set -x
cd /go/src/github.com/IBM/ibm-vpc-file-csi-driver
# Always build for linux/amd64 architecture as the image is supported for linux based systems.
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -ldflags '-X main.vendorVersion='"vpcFileDriver-${TAG}"' -extldflags "-static"' -o /go/bin/ibm-vpc-file-csi-driver ./cmd/
