#!/bin/sh
# ******************************************************************************
# * Licensed Materials - Property of IBM
# * IBM Cloud Kubernetes Service, 5737-D43
# * (C) Copyright IBM Corp. 2023 All Rights Reserved.
# * US Government Users Restricted Rights - Use, duplication or
# * disclosure restricted by GSA ADP Schedule Contract with IBM Corp.
# ******************************************************************************

# The entry point for container
# install driver
set -ex

if [ "$IS_NODE_SERVER" = "true" ]; then
    echo "Inside node-server. Enabling mount-helper-container service..."
    /home/ibm-csi-drivers/systemutil -action reload
    /home/ibm-csi-drivers/systemutil -target mount-helper-container.service -action start
fi

# Run file-csi-driver binary with all the possible arguments passed from the deployment.
/home/ibm-csi-drivers/ibm-vpc-file-csi-driver $1 $2 $3 $4

if [ "$IS_NODE_SERVER" = "true" ]; then
    echo "Stopping mount-helper-container service..."
    /home/ibm-csi-drivers/systemutil -target mount-helper-container.service -action stop
fi

set +ex
