#!/bin/bash

# This script will install kustomize, which is a tool that simplifies patching
# Kubernetes manifests for different environments.
# https://github.com/kubernetes-sigs/kustomize

set -o nounset
set -o errexit

KUSTOMIZE_VERSION="5.3.0"

readonly INSTALL_DIR="${GOPATH}/bin"
readonly KUSTOMIZE_PATH="${INSTALL_DIR}/kustomize"

# If kustomize binary is not found, download and install it.
if [ ! -f "${KUSTOMIZE_PATH}" ]; then
  if [ ! -f "${INSTALL_DIR}" ]; then
    mkdir -p ${INSTALL_DIR}
  fi

  echo "Installing kustomize in ${KUSTOMIZE_PATH}"

  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  if [[ "$OS" != "linux" && "$OS" != "darwin" ]]; then
    echo "Unsupported OS: $OS"
    exit 1
  fi

  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
  esac
  
  curl -L https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv${KUSTOMIZE_VERSION}/kustomize_v${KUSTOMIZE_VERSION}_${OS}_${ARCH}.tar.gz -o kustomize.tar.gz

  tar -xzf kustomize.tar.gz
  chmod +x kustomize
  mv kustomize "${KUSTOMIZE_PATH}"

  rm -f kustomize.tar.gz
else
  echo "Kustomize is already installed at ${KUSTOMIZE_PATH}"
fi
echo "Kustomize version:" 
${KUSTOMIZE_PATH} version
