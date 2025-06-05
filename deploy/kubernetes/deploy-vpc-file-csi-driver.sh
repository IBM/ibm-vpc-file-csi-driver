#!/bin/bash

# Installing VPC file volume CSI Driver to the IKS cluster

set -o nounset
set -o errexit

# Get directory of this script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Call setup-kustomize.sh relative to this script
"${SCRIPT_DIR}/scripts/setup-kustomize.sh"

# Check if the storage-secret-store exists in the kube-system namespace
"${SCRIPT_DIR}/scripts/verify-storage-secret-store.sh"; rc=$?

# Check for arguments
if [ $# != 1 ]; then
  echo "This will install 'development' version of vpc csi driver!"
else
  readonly IKS_VPC_FILE_DRIVER_VERSION=$1
  echo "This will install '${IKS_VPC_FILE_DRIVER_VERSION}' manifests of vpc file csi driver!"
fi

readonly VERSION="${IKS_VPC_FILE_DRIVER_VERSION:-dev}"

# Run kustomize from the script's overlay path
kustomize build "${SCRIPT_DIR}/overlays/${VERSION}" | kubectl apply -f -

# Apply storageclass from same base dir
kubectl apply -f "${SCRIPT_DIR}/storageclass"
