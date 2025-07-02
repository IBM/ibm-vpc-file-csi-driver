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

// GetSecurityGroupForVolumeAccessPoint  get the SecurityGroup based on the request
func (vpcs *VPCSession) GetSecurityGroupForVolumeAccessPoint(securityGroupRequest provider.SecurityGroupRequest) (string, error) {
	vpcs.Logger.Info("Entry of GetSecurityGroupForVolumeAccessPoint method...", zap.Reflect("securityGroupRequest", securityGroupRequest))
	defer vpcs.Logger.Info("Exit from GetSecurityGroupForVolumeAccessPoint method...")

	// Get SecurityGroup by VPC and name. This is inefficient operation which requires iteration over SecurityGroup list
	securityGroup, err := vpcs.getSecurityGroupByVPCAndSecurityGroupName(securityGroupRequest)
	vpcs.Logger.Info("getSecurityGroupByVPCAndSecurityGroupName response", zap.Reflect("securityGroup", securityGroup), zap.Error(err))
	return securityGroup, err
}

func (vpcs *VPCSession) getSecurityGroupByVPCAndSecurityGroupName(securityGroupRequest provider.SecurityGroupRequest) (string, error) {
	vpcs.Logger.Debug("Entry of getSecurityGroupByVPCAndSecurityGroupName()")
	defer vpcs.Logger.Debug("Exit from getSecurityGroupByVPCAndSecurityGroupName()")
	vpcs.Logger.Info("Getting getSecurityGroupByVPCAndSecurityGroupName from VPC provider...")
	var err error
	var start = ""

	filters := &models.ListSecurityGroupFilters{
		ResourceGroupID: securityGroupRequest.ResourceGroup.ID,
		VPCID:           securityGroupRequest.VPCID,
	}

	for {

		securityGroups, err := vpcs.Apiclient.FileShareService().ListSecurityGroups(pageSize, start, filters, vpcs.Logger)

		if err != nil {
			// API call is failed
			return "", userError.GetUserError("SecurityGroupsListFailed", err)
		}

		// Iterate over the SecurityGroup list for given volume
		if securityGroups != nil {
			for _, securityGroupItem := range securityGroups.SecurityGroups {
				// Check if securityGroup is matching with requested input securityGroup name
				if strings.EqualFold(securityGroupRequest.Name, securityGroupItem.Name) {
					vpcs.Logger.Info("Successfully found securityGroup", zap.Reflect("securityGroupItem", securityGroupItem))
					return securityGroupItem.ID, nil
				}
			}

			if securityGroups.Next == nil {
				break // No more pages, exit the loop
			}

			// Fetch the start of next page
			startUrl, err := url.Parse(securityGroups.Next.Href)
			if err != nil {
				// API call is failed
				vpcs.Logger.Warn("The next parameter of the securityGroup list could not be parsed.", zap.Reflect("Next", securityGroups.Next.Href), zap.Error(err))
				return "", userError.GetUserError(string("SecurityGroupFindFailed"), err, securityGroupRequest.Name)
			}

			vpcs.Logger.Info("startUrl", zap.Reflect("startUrl", startUrl))
			start = startUrl.Query().Get("start") //parse query param into map
			if start == "" {
				// API call is failed
				vpcs.Logger.Warn("The start specified in the next parameter of the securityGroup list is empty.", zap.Reflect("startUrl", startUrl))
				return "", userError.GetUserError(string("SecurityGroupFindFailed"), errors.New("no securityGroup found"), securityGroupRequest.Name)
			}
		} else {
			return "", userError.GetUserError(string("SecurityGroupsListFailed"), errors.New("SecurityGroup list is empty"))
		}
	}

	// No volume SecurityGroup found in the  list. So return error
	vpcs.Logger.Error("SecurityGroup not found", zap.Error(err))
	return "", userError.GetUserError(string("SecurityGroupFindFailed"), errors.New("no securityGroup found"), securityGroupRequest.Name)
}
