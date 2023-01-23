/**
 * Copyright 2021 IBM Corp.
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
)

// GetVolumeAccessPoint  get the file share target based on the request
func (vpcs *VPCSession) GetVolumeAccessPoint(volumeAccessPointRequest provider.VolumeAccessPointRequest) (*provider.VolumeAccessPointResponse, error) {
	vpcs.Logger.Debug("Entry of GetVolumeAccessPoint method...", zap.Reflect("volumeAccessPointRequest", volumeAccessPointRequest))
	defer vpcs.Logger.Debug("Exit from GetVolumeAccessPoint method...")
	var err error
	vpcs.Logger.Info("Validating basic inputs for GetVolumeAccessPoint method...", zap.Reflect("volumeAccessPointRequest", volumeAccessPointRequest))
	err = vpcs.validateVolumeAccessPointRequest(volumeAccessPointRequest)
	if err != nil {
		return nil, err
	}
	var volumeAccessPointResponse *provider.VolumeAccessPointResponse
	volumeAccessPoint := models.NewShareTarget(volumeAccessPointRequest)
	if len(volumeAccessPoint.ID) > 0 {
		//Get volume AccessPoint by target ID if it is specified
		volumeAccessPointResponse, err = vpcs.getVolumeAccessPointByID(volumeAccessPoint)
	} else {
		// Get volume AccessPoint by VPC ID. This is inefficient operation which requires iteration over volume target list
		volumeAccessPointResponse, err = vpcs.getVolumeAccessPointByVPCID(volumeAccessPoint)
	}
	vpcs.Logger.Info("Volume access point response", zap.Reflect("volumeAccessPointResponse", volumeAccessPointResponse), zap.Error(err))
	return volumeAccessPointResponse, err
}

func (vpcs *VPCSession) getVolumeAccessPointByID(volumeAccessPointRequest models.ShareTarget) (*provider.VolumeAccessPointResponse, error) {
	vpcs.Logger.Debug("Entry of getVolumeAccessPointByID()")
	defer vpcs.Logger.Debug("Exit from getVolumeAccessPointByID()")
	vpcs.Logger.Info("Getting VolumeAccessPoint from VPC provider...")
	var err error
	var volumeAccessPointResult *models.ShareTarget

	err = vpcs.APIRetry.FlexyRetry(vpcs.Logger, func() (error, bool) {
		volumeAccessPointResult, err = vpcs.Apiclient.FileShareService().GetFileShareTarget(volumeAccessPointRequest.ShareID, volumeAccessPointRequest.ID, vpcs.Logger)
		// Keep retry, until we get the proper volumeAccessPointResponse object
		if err != nil && volumeAccessPointResult == nil {
			return err, skipRetryForObviousErrors(err)
		}
		return err, true // stop retry as no error
	})

	if err != nil {
		// API call is failed
		userErr := userError.GetUserError(string(userError.AccessPointWithAPIDFindFailed), err, volumeAccessPointRequest.ShareID, volumeAccessPointRequest.ID)
		return nil, userErr
	}

	volumeAccessPointResponse := volumeAccessPointResult.ToVolumeAccessPointResponse()
	volumeAccessPointResponse.VolumeID = volumeAccessPointRequest.ShareID

	vpcs.Logger.Info("Successfully retrieved volume AccessPoint", zap.Reflect("volumeAccessPointResponse", volumeAccessPointResponse))
	return volumeAccessPointResponse, err
}

func (vpcs *VPCSession) getVolumeAccessPointByVPCID(volumeAccessPointRequest models.ShareTarget) (*provider.VolumeAccessPointResponse, error) {
	vpcs.Logger.Debug("Entry of getVolumeAccessPointByVPCID()")
	defer vpcs.Logger.Debug("Exit from getVolumeAccessPointByVPCID()")
	vpcs.Logger.Info("Getting VolumeTargetList from VPC provider...")
	var volumeAccessPointList *models.ShareTargetList
	var err error
	err = vpcs.APIRetry.FlexyRetry(vpcs.Logger, func() (error, bool) {
		volumeAccessPointList, err = vpcs.Apiclient.FileShareService().ListFileShareTargets(volumeAccessPointRequest.ShareID, nil, vpcs.Logger)
		// Keep retry, until we get the proper volumeAccessPointResponse object
		if err != nil {
			return err, skipRetryForObviousErrors(err)
		}
		return err, true // stop retry as no error
	})

	if err != nil {
		// API call is failed
		userErr := userError.GetUserError(string(userError.AccessPointWithVPCIDFindFailed), err, volumeAccessPointRequest.ShareID, volumeAccessPointRequest.VPC.ID)
		return nil, userErr
	}
	// Iterate over the volume AccessPoint list for given volume
	if volumeAccessPointList != nil {
		for _, volumeAccessPointItem := range volumeAccessPointList.ShareTargets {
			// Check if VPC ID is matching with requested VPC ID in volume target list
			if volumeAccessPointItem.VPC != nil && volumeAccessPointItem.VPC.ID == volumeAccessPointRequest.VPC.ID {
				vpcs.Logger.Info("Successfully found volume AccessPoint", zap.Reflect("volumeAccessPoint", volumeAccessPointItem))
				volumeAccessPointResponse := volumeAccessPointItem.ToVolumeAccessPointResponse()
				volumeAccessPointResponse.VolumeID = volumeAccessPointRequest.ShareID

				vpcs.Logger.Info("Successfully fetched volume AccessPoint from VPC provider", zap.Reflect("volumeTargetResponse", volumeAccessPointResponse))
				return volumeAccessPointResponse, nil
			}
		}
	}
	// No volume AccessPoint found in the  list. So return error
	userErr := userError.GetUserError(string(userError.AccessPointWithVPCIDFindFailed), errors.New("no volume access point found"), volumeAccessPointRequest.ShareID, volumeAccessPointRequest.VPC.ID)
	vpcs.Logger.Error("Volume AccessPoint not found", zap.Error(err))
	return nil, userErr
}
