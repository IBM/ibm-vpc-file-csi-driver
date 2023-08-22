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

if [ "$IS_NODE_SERVER" == "true" ]; then
    # Copy service files
    mkdir -p  /host/lib/eit-mount-utility
    cp /home/ibm-csi-drivers/eit-mount-utility/* /host/lib/eit-mount-utility/
    cp /home/ibm-csi-drivers/eit-mount-utility/eit-mount-utility.service /host/lib/systemd/system/
    # enable eit-mount-utility.service
    ln -s -f /lib/systemd/system/eit-mount-utility.service  /host/etc/systemd/system/multi-user.target.wants/eit-mount-utility.service

    # /home/ibm-csi-drivers/eit-mount-utility/systemutil -target eit-mount-utility.service -action enable -- This does not work from container. It works on Host directly
    /home/ibm-csi-drivers/eit-mount-utility/systemutil -target eit-mount-utility.service -action start
fi

/home/ibm-csi-drivers/ibm-vpc-file-csi-driver

set +ex
