#!/bin/bash

set -o nounset
set -o errexit

# Get directory of this script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

kubectl get secrets -n kube-system | grep storage-secret-store  > /dev/null 2>&1

# Create secret if it doesnot exist using slclient_Gen2.toml
if [ $? != 0 ]; then
	echo "Creating storage-secret-store in kube-system namespace..."
	if [[ "$OSTYPE" == "linux-gnu"* ]]; then
		echo $OSTYPE	
		encodeVal=$(base64 -w 0 "${SCRIPT_DIR}/../overlays/dev/slclient_Gen2.toml")
			sed -i "s/REPLACE_ME/$encodeVal/g" "${SCRIPT_DIR}/../manifests/storage-secret-store.yaml"

	elif [[ "$OSTYPE" == "darwin"* ]]; then
		echo $OSTYPE
		encodeVal=$(base64 "${SCRIPT_DIR}/../overlays/dev/slclient_Gen2.toml")
			sed -i '.bak' "s/REPLACE_ME/$encodeVal/g" "${SCRIPT_DIR}/../manifests/storage-secret-store.yaml"
	fi
fi

echo "You can install VPC File volume driver..."
