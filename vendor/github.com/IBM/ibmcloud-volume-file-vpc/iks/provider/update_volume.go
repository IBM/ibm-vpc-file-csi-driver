/**
 * Copyright 2024 IBM Corp.
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
	"strconv"
	"time"

	userError "github.com/IBM/ibmcloud-volume-file-vpc/common/messages"
	vpc_provider "github.com/IBM/ibmcloud-volume-file-vpc/file/provider"
	"github.com/IBM/ibmcloud-volume-interface/lib/metrics"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"go.uber.org/zap"
)

const (
	//ClusterIDTagName ...
	ClusterIDTagName = "clusterid"
	//VolumeStatus ...
	VolumeStatus = "status"
)

// UpdateVolume updates the volume with given information
func (vpcIks *IksVpcSession) UpdateVolume(volumeRequest provider.Volume) (err error) {
	vpcIks.Logger.Debug("Entry of UpdateVolume method...")
	defer vpcIks.Logger.Debug("Exit from UpdateVolume method...")
	defer metrics.UpdateDurationFromStart(vpcIks.Logger, "UpdateVolume", time.Now())

	vpcIks.Logger.Info("Basic validation for UpdateVolume request... ", zap.Reflect("RequestedVolumeDetails", volumeRequest))

	// Build the template to send to backend
	pvcTemplate := NewUpdatePVC(volumeRequest)
	err = validateVolumeRequest(volumeRequest)
	if err != nil {
		return err
	}
	vpcIks.Logger.Info("Successfully validated inputs for UpdateVolume request... ")

	vpcIks.Logger.Info("Calling  provider for volume update...")
	err = vpcIks.APIRetry.FlexyRetry(vpcIks.Logger, func() (error, bool) {
		err = vpcIks.IksSession.Apiclient.FileShareService().UpdateVolume(&pvcTemplate, vpcIks.Logger)
		return err, err == nil || vpc_provider.SkipRetryForIKS(err)
	})

	if err != nil {
		vpcIks.Logger.Debug("Failed to update volume", zap.Reflect("BackendError", err))
		return userError.GetUserError("UpdateFailed", err)
	}

	return err
}

// validateVolumeRequest validating volume request
func validateVolumeRequest(volumeRequest provider.Volume) error {
	// Volume name should not be empty
	if len(volumeRequest.VolumeID) == 0 {
		return userError.GetUserError("ErrorRequiredFieldMissing", nil, "VolumeID")
	}
	// Provider name should not be empty
	if len(volumeRequest.Provider) == 0 {
		return userError.GetUserError("ErrorRequiredFieldMissing", nil, "Provider")
	}
	// VolumeType  should not be empty
	if len(volumeRequest.VolumeType) == 0 {
		return userError.GetUserError("ErrorRequiredFieldMissing", nil, "VolumeType")
	}

	return nil
}

// Only for v2/storage/updateVolume
// NewUpdatePVC creates model UpdatePVC from provider volume
func NewUpdatePVC(volumeRequest provider.Volume) provider.UpdatePVC {
	// Build the template to send to backend

	pvc := provider.UpdatePVC{
		ID:         volumeRequest.VolumeID,
		CRN:        volumeRequest.CRN,
		Tags:       volumeRequest.VPCVolume.Tags,
		Provider:   string(volumeRequest.Provider),
		VolumeType: string(volumeRequest.VolumeType),
	}
	if volumeRequest.Name != nil {
		pvc.Name = *volumeRequest.Name
	}
	if volumeRequest.Capacity != nil {
		pvc.Capacity = int64(*volumeRequest.Capacity)
	}

	if volumeRequest.Iops != nil {
		value, err := strconv.ParseInt(*volumeRequest.Iops, 10, 64)
		if err != nil {
			pvc.Iops = 0
		}
		pvc.Iops = value
	}

	pvc.Cluster = volumeRequest.Attributes[ClusterIDTagName]
	pvc.Status = volumeRequest.Attributes[VolumeStatus]

	return pvc
}
