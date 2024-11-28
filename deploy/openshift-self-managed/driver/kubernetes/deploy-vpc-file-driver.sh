#!/bin/bash

# Installing IBM Cloud File Storage Share CSI Driver to the Red Hat OpenShift Container Platform Self-Managed cluster

set -o nounset
set -o errexit
#set -x

if [ $# == 0 ]; then
  echo "This will install 'stable' version of the IBM Cloud File Storage Share CSI Driver!"
else
  readonly IKS_VPC_FILE_DRIVER_VERSION=$1
  echo "This will install '${IKS_VPC_FILE_DRIVER_VERSION}' version of the IBM Cloud File Storage Share CSI Driver!"
fi

readonly VERSION="${IKS_VPC_FILE_DRIVER_VERSION:-stable}"

if [ $# -ge 2 ]; then
  echo "Execution as non-interactive input for ConfigMap objects"
  export EDIT_REQUIRED_MANUAL_IBMCLOUD_ACCOUNT_ID="$2"
  export EDIT_REQUIRED_MANUAL_IBMCLOUD_VPC_ID="$3"
  export EDIT_REQUIRED_MANUAL_IBMCLOUD_VPC_SUBNET_IDS="$4"
  export EDIT_REQUIRED_MANUAL_CLUSTER_ID="$5"
else
  echo "Execution as interactive input prompts for ConfigMap objects"
  echo "Input =  IBMCLOUD_ACCOUNT_ID"
  echo -e ""
  read -p "Value =  " EDIT_REQUIRED_MANUAL_IBMCLOUD_ACCOUNT_ID

  echo "Input =  IBMCLOUD_VPC_ID"
  echo -e ""
  read -p "Value =  " EDIT_REQUIRED_MANUAL_IBMCLOUD_VPC_ID

  echo "Input =  IBMCLOUD_VPC_SUBNET_IDS"
  echo "NOTE     Must use comma-separated string without quotation or spaces such as..."
  echo "EXAMPLE  02b7-11111,02c7-22222,02d7-33333"
  echo -e ""
  read -p "Value =  " EDIT_REQUIRED_MANUAL_IBMCLOUD_VPC_SUBNET_IDS

  echo "Checking for Cluster Name from the control plane node names"
  oc get nodes -l node-role.kubernetes.io/master --output json | jq -r '.items[0].metadata.name'
  echo "Input =  CLUSTER_ID (amend from above)"
  echo -e ""
  read -p "Value =  " EDIT_REQUIRED_MANUAL_CLUSTER_ID

  # Export so can be used by envsubst
  export EDIT_REQUIRED_MANUAL_IBMCLOUD_ACCOUNT_ID
  export EDIT_REQUIRED_MANUAL_IBMCLOUD_VPC_ID
  export EDIT_REQUIRED_MANUAL_IBMCLOUD_VPC_SUBNET_IDS
  export EDIT_REQUIRED_MANUAL_CLUSTER_ID
fi

echo "Checking for existing ConfigMap objects 'cluster-info' and 'ibm-cloud-provider-data'"
configmap1_test=$(oc get configmap ibm-cloud-provider-data --namespace openshift-cluster-csi-drivers 2>&1 >/dev/null || true)
configmap2_test=$(oc get configmap cluster-info --namespace openshift-cluster-csi-drivers 2>&1 >/dev/null || true)

if echo "$configmap1_test" | grep -iq "not found"; then
  echo "Creating 'ibm-cloud-provider-data'"
  envsubst < manual-config-map1.yaml | kubectl apply -f -
else
  echo "No action taken as already existing object 'ibm-cloud-provider-data'"
fi

if echo "$configmap2_test" | grep -iq "not found"; then
  echo "Creating 'cluster-info'"
  envsubst < manual-config-map2.yaml | kubectl apply -f -
else
  echo "No action taken as already existing object 'cluster-info'"
fi


#${KUSTOMIZE_PATH}
kustomize build "${PWD}"/overlays/"${VERSION}" | kubectl apply -f -
