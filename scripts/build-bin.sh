#!/bin/bash
# ******************************************************************************
# * Licensed Materials - Property of IBM
# * IBM Cloud Container Service, 5737-D43
# * (C) Copyright IBM Corp. 2018, 2019 All Rights Reserved.
# * US Government Users Restricted Rights - Use, duplication or
# * disclosure restricted by GSA ADP Schedule Contract with IBM Corp.
# ******************************************************************************

set -e
set +x
set -x
cd /go/src/github.com/IBM/ibm-vpc-file-csi-driver
CGO_ENABLED=0 go build -a -ldflags '-X main.vendorVersion='"vpcFileDriver-${TAG}"' -extldflags "-static"' -o /go/bin/ibm-vpc-file-csi-driver ./cmd/
