/*******************************************************************************
 * IBM Confidential
 * OCO Source Materials
 * IBM Cloud Kubernetes Service, 5737-D43
 * (C) Copyright IBM Corp. 2023 All Rights Reserved.
 * The source code for this program is not published or otherwise divested of
 * its trade secrets, irrespective of what has been deposited with
 * the U.S. Copyright Office.
 ******************************************************************************/

// Package vpcfilevolume ...
package vpcfilevolume

import (
	"time"

	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	"go.uber.org/zap"
)

// GetSubnetByVPCZone GETs /Subnets
func (vs *FileShareService) GetSubnetByVPCZone(zoneName string, vpcID string, resourceGroupID string, ctxLogger *zap.Logger) (*models.Subnet, error) {
	ctxLogger.Debug("Entry Backend GetSubnetByVPCZone")
	defer ctxLogger.Debug("Exit Backend GetSubnetByVPCZone")

	defer util.TimeTracker("GetSubnetByVPCZone", time.Now())

	// Get the  subnet details for a single Subnet, ListSubnetFilters will return all the subnets within the resource group
	filters := &models.ListSubnetFilters{ResourceGroupID: resourceGroupID}
	subnets, err := vs.ListSubnets(100, "", filters, ctxLogger)
	if err != nil {
		return nil, err
	}

	if subnets != nil {
		subnetList := subnets.Subnets
		if len(subnetList) > 0 {
			return subnetList[0], nil
		}
	}
	return nil, err
}
