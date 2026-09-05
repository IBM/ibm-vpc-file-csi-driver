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
	"github.com/IBM/ibmcloud-volume-interface/lib/utils/reasoncode"

	"go.uber.org/zap"
)

// VpcVolumeAccessPoint ...
const (
	StatusStable   = "stable"
	StatusDeleting = "deleting"
	StatusDeleted  = "deleted"
)

// VolumeAccessPoint create volume target based on given volume accessPoint request
func (vpcs *VPCSession) CreateVolumeAccessPoint(volumeAccessPointRequest provider.VolumeAccessPointRequest) (*provider.VolumeAccessPointResponse, error) {
	vpcs.Logger.Debug("Entry of CreateVolumeAccessPoint method...")
	defer vpcs.Logger.Debug("Exit from CreateVolumeAccessPoint method...")
	defer metrics.UpdateDurationFromStart(vpcs.Logger, "CreateVolumeAccessPoint", time.Now())
	var err error
	vpcs.Logger.Info("Validating basic inputs for CreateVolumeAccessPoint method...", zap.Reflect("volumeAccessPointRequest", volumeAccessPointRequest))
	err = vpcs.validateVolumeAccessPointRequest(volumeAccessPointRequest)
	if err != nil {
		return nil, err
	}
	var volumeAccessPointResult *models.ShareTarget
	var varp *provider.VolumeAccessPointResponse

	volumeAccessPoint := models.NewShareTarget(volumeAccessPointRequest)

	err = vpcs.APIRetry.FlexyRetry(vpcs.Logger, func() (error, bool) {
		/*First , check if volume target is already created
		Even if we remove this check RIAAS will respond "shares_target_vpc_duplicate" erro code.
		We need to again do GetVolumeAccessPoint to fetch the already created access point */
		vpcs.Logger.Info("Checking if volume accessPoint is already created by other thread")
		currentVolAccessPoint, err := vpcs.GetVolumeAccessPoint(volumeAccessPointRequest)
		if err == nil && currentVolAccessPoint != nil {
			vpcs.Logger.Info("Volume accessPoint is already created", zap.Reflect("currentVolAccessPoint", currentVolAccessPoint))
			varp = currentVolAccessPoint
			return nil, true // stop retry volume accessPoint already created
		}

		// If ENI/VNI is enabled
		if volumeAccessPointRequest.AccessControlMode == SecurityGroup {
			volumeAccessPoint.VPC = nil // We can either pass VPC or VNI
			volumeAccessPoint.VirtualNetworkInterface = &models.VirtualNetworkInterface{
				SecurityGroups: volumeAccessPointRequest.SecurityGroups,
				ResourceGroup:  volumeAccessPointRequest.ResourceGroup,
			}

			if len(volumeAccessPointRequest.SubnetID) != 0 {
				volumeAccessPoint.VirtualNetworkInterface.Subnet = &models.SubnetRef{
					ID: volumeAccessPointRequest.SubnetID,
				}
			}

			if volumeAccessPointRequest.PrimaryIP != nil {
				volumeAccessPoint.VirtualNetworkInterface.PrimaryIP = volumeAccessPointRequest.PrimaryIP
			}
		}

		//Try creating volume accessPoint if it's not already created or there is error in getting current volume accessPoint
		vpcs.Logger.Info("Creating volume accessPoint from VPC provider...")
		volumeAccessPointResult, err = vpcs.Apiclient.FileShareService().CreateFileShareTarget(&volumeAccessPoint, vpcs.Logger)
		// Keep retry, until we get the proper volumeAccessPointResult object
		if err != nil && volumeAccessPointResult == nil {
			return err, skipRetryForObviousErrors(err)
		}
		varp = volumeAccessPointResult.ToVolumeAccessPointResponse()

		return err, true // stop retry as no error
	})

	if err != nil {
		userErr := userError.GetUserError(string(userError.CreateVolumeAccessPointFailed), err, volumeAccessPointRequest.VolumeID, volumeAccessPointRequest.VPCID)
		return nil, userErr
	}
	vpcs.Logger.Info("Successfully created volume accessPoint from VPC provider", zap.Reflect("volumeAccessPointResponse", varp))
	varp.VolumeID = volumeAccessPointRequest.VolumeID
	return varp, nil
}

// validateVolume validating volume ID and VPC ID
func (vpcs *VPCSession) validateVolumeAccessPointRequest(volumeAccessPointRequest provider.VolumeAccessPointRequest) error {
	var err error
	// Check for VolumeID - required validation
	if len(volumeAccessPointRequest.VolumeID) == 0 {
		err = userError.GetUserError(string(reasoncode.ErrorRequiredFieldMissing), nil, "VolumeID")
		vpcs.Logger.Error("volumeAccessPointRequest.VolumeID is required", zap.Error(err))
		return err
	}

	// Check for VPC ID, SubnetID or AccessPointID  - required validation
	if len(volumeAccessPointRequest.VPCID) == 0 && len(volumeAccessPointRequest.SubnetID) == 0 && len(volumeAccessPointRequest.AccessPointID) == 0 {
		err = userError.GetUserError(string(reasoncode.ErrorRequiredFieldMissing), nil, "VPCID or SubnetID or AccessPointID")
		vpcs.Logger.Error("One of volumeAccessPointRequest.VPCID, volumeAccessPointRequest.SubnetID and volumeAccessPointRequest.AccessPoint is required", zap.Error(err))
		return err
	}

	return nil
}
