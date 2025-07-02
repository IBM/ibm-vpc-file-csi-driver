/**
 * Copyright 2023 IBM Corp.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package provider ...
package provider

import (
	"errors"
	userError "github.com/IBM/ibmcloud-volume-file-vpc/common/messages"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"go.uber.org/zap"
	"net/url"
	"strings"
)

// / GetSubnet  get the subnet based on the request
func (vpcs *VPCSession) GetSubnetForVolumeAccessPoint(subnetRequest provider.SubnetRequest) (string, error) {
	vpcs.Logger.Info("Entry of GetSubnetForVolumeAccessPoint method...", zap.Reflect("subnetRequest", subnetRequest))
	defer vpcs.Logger.Info("Exit from GetSubnetForVolumeAccessPoint method...")

	// Get Subnet by zone and cluster subnet list. This is inefficient operation which requires iteration over subnet list
	subnet, err := vpcs.getSubnetByZoneAndSubnetID(subnetRequest)
	vpcs.Logger.Info("getSubnetByVPCIDAndZone response", zap.Reflect("subnet", subnet), zap.Error(err))
	return subnet, err
}

func (vpcs *VPCSession) getSubnetByZoneAndSubnetID(subnetRequest provider.SubnetRequest) (string, error) {
	vpcs.Logger.Debug("Entry of getSubnetByVPCIDAndZone()")
	defer vpcs.Logger.Debug("Exit from getSubnetByVPCIDAndZone()")
	vpcs.Logger.Info("Getting getSubnetByVPCIDAndZone from VPC provider...")
	var err error
	var start = ""

	filters := &models.ListSubnetFilters{
		ResourceGroupID: subnetRequest.ResourceGroup.ID,
		VPCID:           subnetRequest.VPCID,
		ZoneName:        subnetRequest.ZoneName,
	}

	for {

		subnets, err := vpcs.Apiclient.FileShareService().ListSubnets(pageSize, start, filters, vpcs.Logger)

		if err != nil {
			// API call is failed
			return "", userError.GetUserError("SubnetsListFailed", err)
		}

		// Iterate over the subnet list for given volume
		if subnets != nil {
			for _, subnetItem := range subnets.Subnets {
				// Check if subnet is matching with requested input subnet-list
				if strings.Contains(subnetRequest.SubnetIDList, subnetItem.ID) {
					vpcs.Logger.Info("Successfully found subnet", zap.Reflect("subnetItem", subnetItem))
					return subnetItem.ID, nil
				}
			}

			if subnets.Next == nil {
				break // No more pages, exit the loop
			}

			// Fetch the start of next page
			startUrl, err := url.Parse(subnets.Next.Href)
			if err != nil {
				// API call is failed
				vpcs.Logger.Warn("The next parameter of the subnet list could not be parsed.", zap.Reflect("Next", subnets.Next.Href), zap.Error(err))
				return "", userError.GetUserError(string("SubnetFindFailed"), err, subnetRequest.ZoneName, subnetRequest.SubnetIDList)
			}

			vpcs.Logger.Info("startUrl", zap.Reflect("startUrl", startUrl))
			start = startUrl.Query().Get("start") //parse query param into map
			if start == "" {
				// API call is failed
				vpcs.Logger.Warn("The start specified in the next parameter of the subnet list is empty.", zap.Reflect("start", startUrl))
				return "", userError.GetUserError(string("SubnetFindFailed"), errors.New("no subnet found"), subnetRequest.ZoneName, subnetRequest.SubnetIDList)
			}
		} else {
			return "", userError.GetUserError(string("SubnetsListFailed"), errors.New("Subnet list is empty"))
		}
	}

	// No volume Subnet found in the  list. So return error
	vpcs.Logger.Error("Subnet not found", zap.Error(err))
	return "", userError.GetUserError(string("SubnetFindFailed"), errors.New("no subnet found"), subnetRequest.ZoneName, subnetRequest.SubnetIDList)
}
