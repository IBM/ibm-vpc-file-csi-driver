#!/bin/bash

# Installing VPC file volume CSI Driver to the IKS cluster

set -o nounset
set -o errexit
#set -x

if [ $# != 1 ]; then
  echo "This will install 'stable' version of vpc csi driver!"
else
  readonly IKS_VPC_FILE_DRIVER_VERSION=$1
  echo "This will install '${IKS_VPC_FILE_DRIVER_VERSION}' version of vpc csi driver!"
fi

readonly VERSION="${IKS_VPC_FILE_DRIVER_VERSION:-stable}"

#${KUSTOMIZE_PATH}
kustomize build "${PWD}"/overlays/"${VERSION}" | kubectl apply -f -
