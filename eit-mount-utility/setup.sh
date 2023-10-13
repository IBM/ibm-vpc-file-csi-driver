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
    echo "Copying mount-helper-container files..."
    cp  /home/ibm-csi-drivers/eitserver-linux /host/usr/local/bin/
    cp /home/ibm-csi-drivers/mount-helper-container.service /host/etc/systemd/system/
    echo "Inside node-server. Enabling mount-helper-container service..."
    /home/ibm-csi-drivers/systemutil -action reload
    /home/ibm-csi-drivers/systemutil -target mount-helper-container.service -action start
    # echo "Checking if the service is enabled..."
    # if /home/ibm-csi-drivers/systemutil is-active --quiet mount-helper; then
    #     echo "The 'mount-helper-container' service is running."
    # else
    #     echo "The 'mount-helper' service is not running. Retrying enabling..."
    #     sleep 60
    # fi

    # Add logic to retry the service and confirm if ready. Try to handle it in a way to log the problem. 
    # Try 2-3 retries for 5 min. 
    # how it can be fixed.
    # Node-server should show in events.
fi

# Run file-csi-driver binary with all the possible arguments passed from the deployment.
/home/ibm-csi-drivers/ibm-vpc-file-csi-driver $1 $2 $3 $4

if [ "$IS_NODE_SERVER" = "true" ]; then
    echo "Stopping mount-helper-container service..."
    /home/ibm-csi-drivers/systemutil -target mount-helper-container.service -action stop
fi

set +ex
