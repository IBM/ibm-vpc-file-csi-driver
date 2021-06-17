#!/bin/bash

# This script will install kustomize, which is a tool that simplifies patching
# Kubernetes manifests for different environments.
# https://github.com/kubernetes-sigs/kustomize

set -o nounset
set -o errexit

readonly INSTALL_DIR="${GOPATH}/bin"
readonly KUSTOMIZE_PATH="${INSTALL_DIR}/kustomize"

if [ ! -f "${KUSTOMIZE_PATH}" ]; then
  if [ ! -f "${INSTALL_DIR}" ]; then
    mkdir -p ${INSTALL_DIR}
  fi

  echo "Installing kustomize in ${KUSTOMIZE_PATH}"
  opsys=linux  # or darwin
  curl -s https://api.github.com/repos/kubernetes-sigs/kustomize/releases/tags/v1.0.8 |\
    grep browser_download |\
    grep $opsys |\
    cut -d '"' -f 4 |\
    xargs curl -O -L
  mv kustomize_*_${opsys}_amd64 ${KUSTOMIZE_PATH}
  chmod u+x ${KUSTOMIZE_PATH}
fi
