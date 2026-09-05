/**
 * Copyright 2026 IBM Corp.
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
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	"github.com/IBM/ibmcloud-volume-interface/lib/metrics"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"go.uber.org/zap"
)

func (vpcs *VPCSession) ModifyVolume(modifyVolumeRequest provider.ModifyVolumeRequest) (*provider.ModifyVolumeResponse, error) {
	vpcs.Logger.Debug("Entry of ModifyVolume method...")
	defer vpcs.Logger.Debug("Exit from ModifyVolume method...")
	defer metrics.UpdateDurationFromStart(vpcs.Logger, "ModifyVolume", time.Now())

	isIopsUpdate := modifyVolumeRequest.Iops > 0
	isBandwidthUpdate := modifyVolumeRequest.Bandwidth > 0

	// Return early without a GetVolume round-trip when nothing to update
	if !isIopsUpdate && !isBandwidthUpdate {
		vpcs.Logger.Warn("No updates requested, fetching current volume state")

		existingVolume, err := vpcs.GetVolume(modifyVolumeRequest.VolumeID)
		if err != nil {
			return nil, err
		}

		var currIops int64
		if existingVolume.Iops != nil && *existingVolume.Iops != "" {
			var parseErr error
			currIops, parseErr = strconv.ParseInt(*existingVolume.Iops, 10, 64)
			if parseErr != nil {
				vpcs.Logger.Warn("Failed to parse current IOPS value", zap.String("iops", *existingVolume.Iops), zap.Error(parseErr))
			}
		}

		return &provider.ModifyVolumeResponse{
			Iops:      currIops,
			Bandwidth: existingVolume.Bandwidth,
		}, nil
	}

	vpcs.Logger.Info("Successfully validated inputs for ModifyVolume request... ")

	var newIops int64
	var newBandwidth int32

	if isIopsUpdate {
		newIops = modifyVolumeRequest.Iops
	}
	if isBandwidthUpdate {
		newBandwidth = modifyVolumeRequest.Bandwidth
	}

	shareTemplate := &models.Share{}
	if isIopsUpdate {
		shareTemplate.Iops = newIops
	}
	if isBandwidthUpdate {
		shareTemplate.Bandwidth = newBandwidth
	}

	vpcs.Logger.Info("Calling VPC provider for volume Modify...")
	var (
		share *models.Share
		err   error
	)
	err = retry(vpcs.Logger, func() error {
		share, err = vpcs.Apiclient.FileShareService().ModifyVolume(
			modifyVolumeRequest.VolumeID,
			shareTemplate,
			vpcs.Logger,
		)
		return err
	})

	if err != nil {
		vpcs.Logger.Debug("Failed to modify volume from VPC provider", zap.Reflect("BackendError", err))
		return nil, userError.GetUserError("FailedToModifyVolume", err, modifyVolumeRequest.VolumeID)
	}

	vpcs.Logger.Info("Successfully accepted volume modify request, now waiting for volume state equal to stable")
	err = WaitForValidVolumeState(vpcs, share.ID)
	if err != nil {
		return nil, userError.GetUserError("VolumeNotInValidState", err, share.ID)
	}

	vpcs.Logger.Info("Volume got valid (stable) state", zap.Reflect("VolumeDetails", share))

	return &provider.ModifyVolumeResponse{
		Iops:      share.Iops,
		Bandwidth: share.Bandwidth,
	}, nil
}
