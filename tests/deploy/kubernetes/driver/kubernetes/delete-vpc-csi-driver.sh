#!/bin/bash

# delete VPC file volume CSI Driver to the IKS cluster

set -o nounset
set -o errexit
#set -x

if [ $# != 1 ]; then
  echo "This will delete 'stable' version of vpc csi driver!"
else
  readonly IKS_VPC_FILE_DRIVER_VERSION=$1
  echo "This will delete '${IKS_VPC_FILE_DRIVER_VERSION}' version of vpc csi driver!"
fi

readonly VERSION="${IKS_VPC_FILE_DRIVER_VERSION:-stable}"
readonly PKG_DIR="${GOPATH}/src/github.com/IBM/ibm-vpc-file-csi-driver"

kustomize build ${PKG_DIR}/deploy/kubernetes/driver/kubernetes/overlays/${VERSION} | kubectl delete --ignore-not-found -f -
