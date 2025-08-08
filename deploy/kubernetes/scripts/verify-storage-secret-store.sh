#!/bin/bash

set -o nounset
set -o errexit
set -o pipefail

# Get directory of this script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

SECRET_NAME="storage-secret-store"
NAMESPACE="kube-system"
TOML_FILE="${SCRIPT_DIR}/../overlays/dev/slclient_Gen2.toml"
YAML_FILE="${SCRIPT_DIR}/../manifests/storage-secret-store.yaml"

# Check if the secret exists
if ! kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" > /dev/null 2>&1; then
	if [[ "$OSTYPE" == "linux-gnu"* ]]; then
		encodeVal=$(base64 -w 0 "$TOML_FILE")
		sed -i "s|REPLACE_ME|$encodeVal|g" "$YAML_FILE"
		echo "Updated storage-secret-store.yaml in manifest"
	elif [[ "$OSTYPE" == "darwin"* ]]; then
		encodeVal=$(base64 -i "$TOML_FILE")
		sed -i '.bak' "s|REPLACE_ME|$encodeVal|g" "$YAML_FILE"
		echo "Updated storage-secret-store.yaml in manifest"
	else
		echo "Unsupported OS: $OSTYPE"
		exit 1
	fi
else
	echo "Secret $SECRET_NAME already exists in $NAMESPACE."
fi
