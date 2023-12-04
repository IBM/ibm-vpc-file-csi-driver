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
	"github.com/IBM/ibmcloud-volume-interface/lib/metrics"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	"go.uber.org/zap"
)

// WaitForDeleteVolumeAccessPoint waits for file share target to be deleted. e.g waits till no file share target is found
func (vpcs *VPCSession) WaitForDeleteVolumeAccessPoint(deleteAccessPointRequest provider.VolumeAccessPointRequest) error {
	vpcs.Logger.Debug("Entry of WaitForDeleteVolumeAccessPoint method...")
	defer vpcs.Logger.Debug("Exit from WaitForDeleteVolumeAccessPoint method...")
	defer metrics.UpdateDurationFromStart(vpcs.Logger, "WaitForDeleteVolumeAccessPoint", time.Now())
	var err error
	vpcs.Logger.Info("Validating basic inputs for WaitForDeleteVolumeAccessPoint method...", zap.Reflect("deleteAccessPointRequest", deleteAccessPointRequest))
	err = vpcs.validateVolumeAccessPointRequest(deleteAccessPointRequest)
	if err != nil {
		return err
	}

	err = vpcs.APIRetry.FlexyRetryWithConstGap(vpcs.Logger, func() (error, bool) {
		_, err := vpcs.GetVolumeAccessPoint(deleteAccessPointRequest)
		// In case of error we should not retry as there are two conditions for error
		// 1- some issues at endpoint side --> Which is already covered in vpcs.GetVolumeAccessPoint
		// 2- AccessPoint not found i.e err != nil --> in this case we should not re-try as it has been deleted
		if err != nil {
			return err, true
		}
		return err, false
	})

	// Could be a success case
	if err != nil {
		if errMsg, ok := err.(util.Message); ok {
			if errMsg.Code == userError.AccessPointWithAPIDFindFailed {
				vpcs.Logger.Info("Volume AccessPoint delete is complete")
				return nil
			}
		}
	}

	userErr := userError.GetUserError(string(userError.DeleteVolumeAccessPointTimedOut), err, deleteAccessPointRequest.VolumeID, deleteAccessPointRequest.AccessPointID)
	vpcs.Logger.Info("Wait for delete AccessPoint timed out", zap.Error(userErr))
	return userErr
}
