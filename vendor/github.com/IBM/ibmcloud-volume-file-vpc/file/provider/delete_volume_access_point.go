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
	userError "github.com/IBM/ibmcloud-volume-file-vpc/common/messages"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	"github.com/IBM/ibmcloud-volume-interface/lib/metrics"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"

	"net/http"
	"time"

	"go.uber.org/zap"
)

// DeleteVolumeAccessPoint deletes file share target for given volume VolumeAccessPoint request
func (vpcs *VPCSession) DeleteVolumeAccessPoint(deleteAccessPointRequest provider.VolumeAccessPointRequest) (*http.Response, error) {
	vpcs.Logger.Debug("Entry of DeleteVolumeAccessPoint method...")
	defer vpcs.Logger.Debug("Exit from DeleteVolumeAccessPoint method...")
	defer metrics.UpdateDurationFromStart(vpcs.Logger, "DeleteVolumeAccessPoint", time.Now())
	var err error
	vpcs.Logger.Info("Validating basic inputs for delete AccessPoint method...", zap.Reflect("deleteAccessPointRequest", deleteAccessPointRequest))
	err = vpcs.validateVolumeAccessPointRequest(deleteAccessPointRequest)
	if err != nil {
		return nil, err
	}

	var response *http.Response
	var volumeAccessPoint models.ShareTarget

	err = vpcs.APIRetry.FlexyRetry(vpcs.Logger, func() (error, bool) {
		// First , check if volume AccessPoint is already deleted to given instance
		vpcs.Logger.Info("Checking if volume AccessPoint is already deleted ")
		currentVolumeAccessPoint, err := vpcs.GetVolumeAccessPoint(deleteAccessPointRequest)
		if err == nil && currentVolumeAccessPoint != nil && currentVolumeAccessPoint.Status != StatusDeleting && currentVolumeAccessPoint.Status != StatusDeleted {
			// If no error and current volume AccessPoint is not already in deleting or deleted state ( i.e in stable or pending state) attempt to delete
			vpcs.Logger.Info("Found volume AccessPoint", zap.Reflect("currentVolAccessPoint", currentVolumeAccessPoint))
			volumeAccessPoint := models.NewShareTarget(deleteAccessPointRequest)
			volumeAccessPoint.ShareID = currentVolumeAccessPoint.VolumeID
			volumeAccessPoint.ID = currentVolumeAccessPoint.AccessPointID
			vpcs.Logger.Info("Deleting volume AccessPoint from VPC provider...")
			response, err = vpcs.Apiclient.FileShareService().DeleteFileShareTarget(&volumeAccessPoint, vpcs.Logger)

			//Retry in case of all errors
			if err != nil {
				return err, false
			}
		}
		vpcs.Logger.Info("No volume access point found for", zap.Reflect("currentVolumeAccessPoint", currentVolumeAccessPoint), zap.Error(err))
		// consider volume delete success if its  already  in deleting, pending deletion or VolumeAccessPoint is not found
		response = &http.Response{
			StatusCode: http.StatusOK,
		}
		return nil, true // skip retry if volume AccessPoint is not found OR already in deleting, pending deletion state
	})
	if err != nil {
		userErr := userError.GetUserError(string(userError.DeleteVolumeAccessPointFailed), err, deleteAccessPointRequest.VolumeID, volumeAccessPoint.ID)
		vpcs.Logger.Error("Volume AccessPoint delete failed with error", zap.Error(err))
		return response, userErr
	}
	vpcs.Logger.Info("Successfully deleted volume AccessPoint from VPC provider", zap.Reflect("resp", response))
	return response, nil
}
