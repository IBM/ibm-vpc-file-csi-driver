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
	vpcfile "github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/vpcfilevolume"
	"github.com/IBM/ibmcloud-volume-interface/lib/metrics"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"go.uber.org/zap"
)

const (
	minSize    = 10 //10 GB
	dp2Profile = "dp2"
)

// CreateVolume creates file share
func (vpcs *VPCSession) CreateVolume(volumeRequest provider.Volume) (volumeResponse *provider.Volume, err error) {
	vpcs.Logger.Debug("Entry of CreateVolume method...")
	defer vpcs.Logger.Debug("Exit from CreateVolume method...")
	defer metrics.UpdateDurationFromStart(vpcs.Logger, "CreateVolume", time.Now())

	var iops int64
	var bandwidth int32
	vpcs.Logger.Info("Basic validation for CreateVolume request... ", zap.Reflect("RequestedVolumeDetails", volumeRequest))
	resourceGroup, iops, bandwidth, err := validateVolumeRequest(volumeRequest)
	if err != nil {
		return nil, err
	}

	vpcs.Logger.Info("Successfully validated inputs for CreateVolume request... ")

	// Set zone if provided
	var zone *models.Zone
	if volumeRequest.Az != "" {
		zone = &models.Zone{
			Name: volumeRequest.Az,
		}
	}

	// Build the share template to send to backend
	shareTemplate := &models.Share{
		Name:              *volumeRequest.Name,
		Size:              int64(*volumeRequest.Capacity),
		InitialOwner:      (*models.InitialOwner)(volumeRequest.InitialOwner),
		Iops:              iops,
		Bandwidth:         bandwidth,
		AccessControlMode: volumeRequest.AccessControlMode,
		ResourceGroup:     &resourceGroup,
		Profile: &models.Profile{
			Name: volumeRequest.VPCVolume.Profile.Name,
		},
		Zone: zone,
	}

	// Check for VPC ID, SubnetID or PrimaryIPID either of the one is mandatory for VolumeAccessPoint/FileShareTarget creation
	// If AccessControlMode is vpc then VPCID is mandatory
	// If AccessControlMode is security_group either subnetID or primaryIPID is mandatory
	if len(volumeRequest.VPCID) != 0 || len(volumeRequest.SubnetID) != 0 || (volumeRequest.PrimaryIP != nil && len(volumeRequest.PrimaryIP.ID) != 0) {

		//Build File Share target template to send to backend
		shareTargetTemplate := models.ShareTarget{
			Name: *volumeRequest.Name,
		}

		// if VNI enabled
		if volumeRequest.AccessControlMode == SecurityGroup {
			setENIParameters(&shareTargetTemplate, volumeRequest)
		} else { // If VPC Mode is enabled.
			shareTargetTemplate.VPC = &provider.VPC{
				ID: volumeRequest.VPCID,
			}
		}

		// This is mandatory property to be set
		shareTargetTemplate.AccessProtocol = "nfs4"

		//Set transit_encryption to ipsec, none, stunnel
		shareTargetTemplate.TransitEncryption = volumeRequest.TransitEncryption

		volumeAccessPointList := make([]models.ShareTarget, 1)
		volumeAccessPointList[0] = shareTargetTemplate

		shareTemplate.ShareTargets = &volumeAccessPointList
	}

	var encryptionKeyCRN string
	if volumeRequest.VPCVolume.VolumeEncryptionKey != nil && len(volumeRequest.VPCVolume.VolumeEncryptionKey.CRN) > 0 {
		encryptionKeyCRN = volumeRequest.VPCVolume.VolumeEncryptionKey.CRN
		shareTemplate.EncryptionKey = &models.EncryptionKey{CRN: encryptionKeyCRN}
	}

	// adding snapshot CRN and ID in the request, if it is provided to create the volume from snapshot
	if len(volumeRequest.SnapshotCRN) > 0 {
		shareTemplate.SourceSnapshot = &models.Snapshot{CRN: volumeRequest.SnapshotCRN}
	} else if len(volumeRequest.SnapshotID) > 0 {
		shareTemplate.SourceSnapshot = &models.Snapshot{ID: volumeRequest.SnapshotID}
	}

	// We dont need zone and AccessControlMode if sourceSnapshot is present
	if shareTemplate.SourceSnapshot != nil {
		shareTemplate.Zone = nil
		shareTemplate.AccessControlMode = ""
	}

	vpcs.Logger.Info("Calling VPC provider for volume creation...")
	var volume *models.Share

	err = retry(vpcs.Logger, func() error {
		volume, err = vpcs.Apiclient.FileShareService().CreateFileShare(shareTemplate, vpcs.Logger)
		return err
	})

	if err != nil {
		vpcs.Logger.Debug("Failed to create volume from VPC provider", zap.Reflect("BackendError", err))
		return nil, userError.GetUserError("FailedToPlaceOrder", err)
	}

	vpcs.Logger.Info("Successfully created volume from VPC provider...", zap.Reflect("VolumeDetails", volume))

	vpcs.Logger.Info("Waiting for volume to be in valid (stable) state", zap.Reflect("VolumeDetails", volume))
	err = WaitForValidVolumeState(vpcs, volume.ID)
	if err != nil {
		return nil, userError.GetUserError("VolumeNotInValidState", err, volume.ID)
	}

	vpcs.Logger.Info("Volume got valid (stable) state", zap.Reflect("VolumeDetails", volume))

	// Converting share to lib volume type
	volumeResponse = FromProviderToLibVolume(volume, vpcs.Logger)
	// VPC does have region yet . So use requested region in response
	volumeResponse.Region = volumeRequest.Region

	/* // TBD Return reuested tag as is if not tags returned by backend
	if len(volumeResponse.Tags) == 0 && len(volumeRequest.Tags) > 0 {
		volumeResponse.Tags = volumeRequest.Tags
	} */
	vpcs.Logger.Info("VolumeResponse", zap.Reflect("volumeResponse", volumeResponse))

	return volumeResponse, err
}

// validateVolumeRequest validating volume request
func validateVolumeRequest(volumeRequest provider.Volume) (models.ResourceGroup, int64, int32, error) {
	resourceGroup := models.ResourceGroup{}
	var iops int64
	iops = 0
	var bandwidth int32
	bandwidth = 0

	// Volume name should not be empty
	if volumeRequest.Name == nil || len(*volumeRequest.Name) == 0 {
		return resourceGroup, iops, bandwidth, userError.GetUserError("InvalidVolumeName", nil, nil)
	}

	if volumeRequest.VPCVolume.Profile == nil {
		return resourceGroup, iops, bandwidth, userError.GetUserError("VolumeProfileEmpty", nil)
	}

	// validate and add resource group ID or Name whichever is provided by user
	if volumeRequest.VPCVolume.ResourceGroup == nil {
		return resourceGroup, iops, bandwidth, userError.GetUserError("EmptyResourceGroup", nil)
	}

	// validate and add resource group ID or Name whichever is provided by user
	if len(volumeRequest.VPCVolume.ResourceGroup.ID) == 0 && len(volumeRequest.VPCVolume.ResourceGroup.Name) == 0 {
		return resourceGroup, iops, bandwidth, userError.GetUserError("EmptyResourceGroupIDandName", nil)
	}

	if len(volumeRequest.VPCVolume.ResourceGroup.ID) > 0 {
		resourceGroup.ID = volumeRequest.VPCVolume.ResourceGroup.ID
	}
	if len(volumeRequest.VPCVolume.ResourceGroup.Name) > 0 {
		// get the resource group ID from resource group name as Name is not supported by RIaaS
		resourceGroup.Name = volumeRequest.VPCVolume.ResourceGroup.Name
	}

	if volumeRequest.Capacity == nil {
		return resourceGroup, iops, bandwidth, userError.GetUserError("VolumeCapacityInvalid", nil, nil)
	}

	// Minimum Capacity validation for non RFS profiles.
	if *volumeRequest.Capacity < minSize && volumeRequest.VPCVolume.Profile.Name != vpcfile.RFSProfile {
		return resourceGroup, iops, bandwidth, userError.GetUserError("VolumeCapacityInvalid", nil, *volumeRequest.Capacity)
	}

	if volumeRequest.Iops != nil {
		iops = ToInt64(*volumeRequest.Iops)
	}

	bandwidth = volumeRequest.VPCVolume.Bandwidth

	return resourceGroup, iops, bandwidth, nil
}

func setENIParameters(shareTarget *models.ShareTarget, volumeRequest provider.Volume) {
	shareTarget.VirtualNetworkInterface = &models.VirtualNetworkInterface{
		SecurityGroups: volumeRequest.SecurityGroups,
		ResourceGroup:  volumeRequest.ResourceGroup,
	}

	if len(volumeRequest.SubnetID) != 0 {
		shareTarget.VirtualNetworkInterface.Subnet = &models.SubnetRef{
			ID: volumeRequest.SubnetID,
		}
	}

	if volumeRequest.PrimaryIP != nil {
		shareTarget.VirtualNetworkInterface.PrimaryIP = volumeRequest.PrimaryIP
	}
}
