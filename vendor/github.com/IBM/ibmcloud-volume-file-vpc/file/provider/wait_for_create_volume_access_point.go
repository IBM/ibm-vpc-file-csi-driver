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
	"go.uber.org/zap"
)

// WaitForCreateVolumeAccessPoint checks if file share target is created and is stable state
func (vpcs *VPCSession) WaitForCreateVolumeAccessPoint(AccessPointRequest provider.VolumeAccessPointRequest) (*provider.VolumeAccessPointResponse, error) {
	vpcs.Logger.Debug("Entry of WaitForCreateVolumeAccessPoint file method...")
	defer vpcs.Logger.Debug("Exit from WaitForCreateVolumeAccessPoint file method...")
	defer metrics.UpdateDurationFromStart(vpcs.Logger, "WaitForCreateVolumeAccessPoint", time.Now())

	vpcs.Logger.Info("Getting volume target details from VPC file provider...", zap.Reflect("VolumeID", AccessPointRequest.VolumeID), zap.Reflect("VPCID", AccessPointRequest.VPCID))

	vpcs.Logger.Info("Validating basic inputs for WaitForCreateVolumeAccessPoint method...", zap.Reflect("volumeAccessPointTemplate", AccessPointRequest))
	err := vpcs.validateVolumeAccessPointRequest(AccessPointRequest)
	if err != nil {
		return nil, err
	}

	var currentVolAccessPoint *provider.VolumeAccessPointResponse
	err = vpcs.APIRetry.FlexyRetryWithConstGap(vpcs.Logger, func() (error, bool) {
		currentVolAccessPoint, err = vpcs.GetVolumeAccessPoint(AccessPointRequest)
		if err != nil {
			// Need to stop retry as there is an error while getting volume target
			// considering that vpcs.GetVolumeAccessPoint already re-tried
			return err, true
		}
		// Stop retry in case of volume target is stable
		return err, currentVolAccessPoint != nil && currentVolAccessPoint.Status == StatusStable
	})

	// Success case, checks are required in case of timeout happened and volume is still not attached state
	if err == nil && (currentVolAccessPoint != nil && currentVolAccessPoint.Status == StatusStable) {
		return currentVolAccessPoint, nil
	}

	userErr := userError.GetUserError(string(userError.CreateVolumeAccessPointTimedOut), nil, AccessPointRequest.VolumeID, AccessPointRequest.AccessPointID)
	vpcs.Logger.Info("Wait for AccessPoint creation timed out", zap.Error(userErr))

	return nil, userErr
}
