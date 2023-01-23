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
	"time"

	userError "github.com/IBM/ibmcloud-volume-file-vpc/common/messages"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	"github.com/IBM/ibmcloud-volume-interface/lib/metrics"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"go.uber.org/zap"
)

// DeleteVolume deletes the file share
func (vpcs *VPCSession) DeleteVolume(volume *provider.Volume) (err error) {
	vpcs.Logger.Debug("Entry of DeleteVolume method...")
	defer vpcs.Logger.Debug("Exit from DeleteVolume method...")
	defer metrics.UpdateDurationFromStart(vpcs.Logger, "DeleteVolume", time.Now())

	vpcs.Logger.Info("Validating basic inputs for DeleteVolume method...", zap.Reflect("VolumeDetails", volume))
	err = validateVolume(volume)
	if err != nil {
		return err
	}

	existingVol, err := vpcs.GetVolume(volume.VolumeID)
	if err != nil {
		return err
	}

	//If there exists any access point for volume we should abort delete
	if existingVol.VolumeAccessPoints != nil && len(*existingVol.VolumeAccessPoints) != 0 {
		var vpcIDList = []string{}
		for _, volAccessPoint := range *existingVol.VolumeAccessPoints {
			if volAccessPoint.VPC != nil {
				vpcIDList = append(vpcIDList, volAccessPoint.VPC.ID)
			}
		}
		return userError.GetUserError(string(userError.VolumeAccessPointExist), nil, volume.VolumeID, vpcIDList)
	}

	vpcs.Logger.Info("Deleting file share from VPC provider...")
	err = retry(vpcs.Logger, func() error {
		vpcs.Logger.Info("Calling VPC client for file share deletion...")
		err = vpcs.Apiclient.FileShareService().DeleteFileShare(volume.VolumeID, vpcs.Logger)
		return err
	})
	if err != nil {
		return userError.GetUserError("FailedToDeleteVolume", err, volume.VolumeID)
	}

	err = WaitForVolumeDeletion(vpcs, volume.VolumeID)
	if err != nil {
		return userError.GetUserError("FailedToDeleteVolume", err, volume.VolumeID)
	}

	vpcs.Logger.Info("Successfully deleted volume from VPC provider")
	return err
}

// validateVolume validating volume ID
func validateVolume(volume *provider.Volume) (err error) {
	if volume == nil {
		err = userError.GetUserError("InvalidVolumeID", nil, nil)
		return
	}

	if IsValidVolumeIDFormat(volume.VolumeID) {
		return nil
	}
	err = userError.GetUserError("InvalidVolumeID", nil, volume.VolumeID)
	return
}

// WaitForVolumeDeletion checks the volume for valid status
func WaitForVolumeDeletion(vpcs *VPCSession, volumeID string) (err error) {
	vpcs.Logger.Debug("Entry of WaitForVolumeDeletion method...")
	defer vpcs.Logger.Debug("Exit from WaitForVolumeDeletion method...")
	var skip = false

	vpcs.Logger.Info("Getting volume details from VPC provider...", zap.Reflect("VolumeID", volumeID))

	err = vpcs.APIRetry.FlexyRetry(vpcs.Logger, func() (error, bool) {
		_, err = vpcs.Apiclient.FileShareService().GetFileShare(volumeID, vpcs.Logger)
		// Keep retry, until GetVolume returns volume not found
		if err != nil {
			skip = skipRetry(err.(*models.Error))
			return nil, skip
		}
		return err, false // continue retry as we are not seeing error which means volume is stable
	})

	if err == nil && skip {
		vpcs.Logger.Info("Volume got deleted.", zap.Reflect("volumeID", volumeID))
	}
	return err
}
