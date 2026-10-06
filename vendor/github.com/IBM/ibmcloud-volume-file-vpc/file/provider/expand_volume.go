/**
 * Copyright 2022 IBM Corp.
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

// GiB ...
const (
	GiB = 1024 * 1024 * 1024
)

func (vpcs *VPCSession) ExpandVolume(expandVolumeRequest provider.ExpandVolumeRequest) (size int64, err error) {
	vpcs.Logger.Debug("Entry of ExpandVolume method...")
	defer vpcs.Logger.Debug("Exit from ExpandVolume method...")
	defer metrics.UpdateDurationFromStart(vpcs.Logger, "ExpandVolume", time.Now())

	// Get volume details
	existingVolume, err := vpcs.GetVolume(expandVolumeRequest.VolumeID)
	if err != nil {
		return -1, err
	}
	// Return existing Capacity if its greater or equal to expandable size
	if existingVolume.Capacity != nil && int64(*existingVolume.Capacity) >= expandVolumeRequest.Capacity {
		vpcs.Logger.Warn("Requested size is less than current size.", zap.Reflect("Current Size: ", existingVolume.VolumeID), zap.Reflect("Requested Size: ", expandVolumeRequest.Capacity))
		return int64(*existingVolume.Capacity), nil
	}
	vpcs.Logger.Info("Successfully validated inputs for ExpandVolume request... ")

	newSize := roundUpSize(expandVolumeRequest.Capacity, GiB)

	// Build the template to send to backend
	shareTemplate := &models.Share{
		Size: newSize,
	}

	vpcs.Logger.Info("Calling VPC provider for volume expand...")
	var share *models.Share
	err = retry(vpcs.Logger, func() error {
		share, err = vpcs.Apiclient.FileShareService().ExpandVolume(expandVolumeRequest.VolumeID, shareTemplate, vpcs.Logger)
		return err
	})

	if err != nil {
		vpcs.Logger.Debug("Failed to expand volume from VPC provider", zap.Reflect("BackendError", err))
		return -1, userError.GetUserError("FailedToExpandVolume", err, expandVolumeRequest.VolumeID)
	}

	vpcs.Logger.Info("Successfully accepted volume expansion request, now waiting for volume state equal to stable")
	err = WaitForValidVolumeState(vpcs, share.ID)
	if err != nil {
		return -1, userError.GetUserError("VolumeNotInValidState", err, share.ID)
	}

	vpcs.Logger.Info("Volume got valid (stable) state", zap.Reflect("VolumeDetails", share))
	return expandVolumeRequest.Capacity, nil
}
