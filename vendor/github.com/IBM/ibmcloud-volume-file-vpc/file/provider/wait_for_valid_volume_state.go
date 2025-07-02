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
	"go.uber.org/zap"
)

// WaitForValidVolumeState checks the file share for valid status
func WaitForValidVolumeState(vpcs *VPCSession, volumeID string) (err error) {
	vpcs.Logger.Debug("Entry of WaitForValidVolumeState file method...")
	defer vpcs.Logger.Debug("Exit from WaitForValidVolumeState file method...")
	defer metrics.UpdateDurationFromStart(vpcs.Logger, "WaitForValidVolumeState", time.Now())

	vpcs.Logger.Info("Getting file share details from VPC file provider...", zap.Reflect("VolumeID", volumeID))

	var volume *models.Share
	err = retry(vpcs.Logger, func() error {
		volume, err = vpcs.Apiclient.FileShareService().GetFileShare(volumeID, vpcs.Logger)
		if err != nil {
			return err
		}
		vpcs.Logger.Info("Getting file share details from VPC provider...", zap.Reflect("volume", volume))
		if volume != nil && volume.Status == StatusStable {
			vpcs.Logger.Info("Volume got valid (stable) state", zap.Reflect("VolumeDetails", volume))
			return nil
		}
		return userError.GetUserError("VolumeNotInValidState", err, volumeID)
	})

	if err != nil {
		vpcs.Logger.Info("Volume could not get valid (stable) state", zap.Reflect("VolumeDetails", volume))
		return userError.GetUserError("VolumeNotInValidState", err, volumeID)
	}

	return nil
}
