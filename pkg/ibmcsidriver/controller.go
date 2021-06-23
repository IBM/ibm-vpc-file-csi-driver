/**
 *
 * Copyright 2021- IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache2.0
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

//Package ibmcsidriver ...
package ibmcsidriver

import (
	"os"
	"strings"
	"time"

	commonError "github.com/IBM/ibm-csi-common/pkg/messages"
	"github.com/IBM/ibm-csi-common/pkg/metrics"
	"github.com/IBM/ibm-csi-common/pkg/utils"
	fileVpcError "github.com/IBM/ibmcloud-volume-file-vpc/common/messages"
	cloudProvider "github.com/IBM/ibmcloud-volume-file-vpc/ibmcloudprovider"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	providerError "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	csi "github.com/container-storage-interface/spec/lib/go/csi"

	"go.uber.org/zap"
	"golang.org/x/net/context"
)

// CSIControllerServer ...
type CSIControllerServer struct {
	Driver      *IBMCSIDriver
	CSIProvider cloudProvider.CloudProviderInterface
}

const (
	// PublishInfoVolumeID ...
	PublishInfoVolumeID = "volume-id"

	// PublishInfoNodeID ...
	PublishInfoNodeID = "node-id"

	// PublishInfoRequestID ...
	PublishInfoRequestID = "request-id"
)

var _ csi.ControllerServer = &CSIControllerServer{}

// CreateVolume ...
func (csiCS *CSIControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	ctx = context.WithValue(ctx, provider.RequestID, requestID)
	ctxLogger.Info("CSIControllerServer-CreateVolume... ", zap.Reflect("Request", *req))
	defer metrics.UpdateDurationFromStart(ctxLogger, "CreateVolume", time.Now())

	// Check basic parameters validations i.e PVC name given
	name := req.GetName()
	if len(name) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.MissingVolumeName, requestID, nil)
	}

	// check volume capabilities
	volumeCapabilities := req.GetVolumeCapabilities()
	if len(volumeCapabilities) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.NoVolumeCapabilities, requestID, nil)
	}

	// Validate volume capabilities, are all capabilities supported by driver or not
	if !areVolumeCapabilitiesSupported(req.GetVolumeCapabilities(), csiCS.Driver.vcap) {
		return nil, commonError.GetCSIError(ctxLogger, commonError.VolumeCapabilitiesNotSupported, requestID, nil)
	}

	// Get volume input Parameters
	requestedVolume, err := getVolumeParameters(ctxLogger, req, csiCS.CSIProvider.GetConfig())
	if requestedVolume != nil {
		// For logging mask VolumeEncryptionKey
		// Create copy of the requestedVolume
		tempReqVol := (*requestedVolume)
		// Mask VolumeEncryptionKey
		tempReqVol.VPCVolume.VolumeEncryptionKey = &provider.VolumeEncryptionKey{CRN: "********"}
		ctxLogger.Info("Volume request after masking encryption key", zap.Reflect("Volume", tempReqVol))
	}

	if err != nil {
		ctxLogger.Error("Unable to extract parameters", zap.Error(err))
		return nil, commonError.GetCSIError(ctxLogger, commonError.InvalidParameters, requestID, err)
	}

	// TODO: Determine Zones and Region for the disk

	// Validate if volume Already Exists
	session, err := csiCS.CSIProvider.GetProviderSession(ctx, ctxLogger)
	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
	}

	var isVolumeExist bool = false

	volumeObj, err := checkIfVolumeExists(session, *requestedVolume, ctxLogger)
	if volumeObj != nil && err == nil {
		ctxLogger.Info("Volume already exists", zap.Reflect("ExistingVolume", volumeObj))
		if volumeObj.Capacity != nil && requestedVolume.Capacity != nil && *volumeObj.Capacity == *requestedVolume.Capacity {
			isVolumeExist = true
		} else {
			return nil, commonError.GetCSIError(ctxLogger, commonError.VolumeAlreadyExists, requestID, err, name, *requestedVolume.Capacity)
		}
	}

	// Create volume if it does no exist
	if !isVolumeExist {
		ctxLogger.Info("Creating Volume...")

		volumeObj, err = session.CreateVolume(*requestedVolume)
		if err != nil {
			return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err, "creation")
		}

		ctxLogger.Info("Volume Created", zap.Reflect("Volume", volumeObj))
	}

	volumeAccesspointReq := provider.VolumeAccessPointRequest{
		VolumeID: volumeObj.VolumeID,
		VPCID:    os.Getenv("VPC_ID"),
	}

	//Create VolumeAccess Point
	//No need to check for access point existence as library takes care of the same
	volumeAccessPointObj, err := createVolumeAccessPoint(session, volumeAccesspointReq, ctxLogger)

	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
	}

	// return csi volume object
	return createCSIVolumeResponse(*volumeObj, *volumeAccessPointObj, int64(*(requestedVolume.Capacity)*utils.GB), nil, csiCS.CSIProvider.GetClusterInfo().ClusterID), nil
}

// DeleteVolume ...
func (csiCS *CSIControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	ctx = context.WithValue(ctx, provider.RequestID, requestID)
	defer metrics.UpdateDurationFromStart(ctxLogger, "DeleteVolume", time.Now())
	ctxLogger.Info("CSIControllerServer-DeleteVolume... ", zap.Reflect("Request", *req))

	// Validate arguments
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.EmptyVolumeID, requestID, nil)
	}

	// TODO:~ Following could be enhancement although currect way is working fine
	// Get the volume name by using volume ID
	// and delete volume by name

	// get the session
	session, err := csiCS.CSIProvider.GetProviderSession(ctx, ctxLogger)
	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.FailedPrecondition, requestID, err)
	}

	//Volume ID is in format volumeID:volumeAccessPointID, to assit the deletion of volume access point
	tokens := strings.Split(volumeID, ":")
	if len(tokens) != 2 {
		ctxLogger.Info("CSIControllerServer-DeleteVolume...", zap.Reflect("Volume ID is not in format volumeID:accesspointID", tokens))
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, nil)
	}

	volume := &provider.Volume{}
	volume.VolumeID = tokens[0]

	existingVol, err := checkIfVolumeExists(session, *volume, ctxLogger)
	if existingVol == nil && err == nil {
		ctxLogger.Info("Volume not found. Returning success without deletion...")
		return &csi.DeleteVolumeResponse{}, nil
	}

	//TBD Do we really have to handle volume with multiple access points per VPC/Subnet
	//If there are more than one access point as of now we will be aborting delete
	if existingVol.VolumeAccessPoints != nil && len(*existingVol.VolumeAccessPoints) > 1 {
		var vpcIDList = []string{}
		for _, volAccessPoint := range *existingVol.VolumeAccessPoints {
			if volAccessPoint.VPC != nil {
				vpcIDList = append(vpcIDList, volAccessPoint.VPC.ID)
			}
		}
		err := fileVpcError.GetUserError(fileVpcError.MultipleVolAccessPointFound, nil, vpcIDList)
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
	}

	//If volume exists no need to check for access point existence as library takes care of the same
	volumeAccesspointReq := provider.VolumeAccessPointRequest{
		VolumeID:      volume.VolumeID,
		AccessPointID: tokens[1],
	}

	ctxLogger.Info("Deleting VolumeAccessPoint...")

	response, err := session.DeleteVolumeAccessPoint(volumeAccesspointReq)
	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
	}

	ctxLogger.Info("DeleteVolumeAccessPoint response", zap.Reflect("response", response))

	err = session.WaitForDeleteVolumeAccessPoint(volumeAccesspointReq)
	if err != nil {
		//retry gap is constant in the common lib i.e 10 seconds and number of retries are 4*Retry configure in the driver
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
	}

	ctxLogger.Info("VolumeAccessPoint deleted successfully")

	ctxLogger.Info("Deleting Volume...")

	err = session.DeleteVolume(volume)
	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
	}

	ctxLogger.Info("Volume deleted successfully")

	return &csi.DeleteVolumeResponse{}, nil
}

// ControllerPublishVolume ...
func (csiCS *CSIControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)

	ctxLogger.Info("CSIControllerServer-ControllerPublishVolume", zap.Reflect("Request", *req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "PublishVolume")
}

// ControllerUnpublishVolume ...
func (csiCS *CSIControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)

	ctxLogger.Info("CSIControllerServer-ControllerUnpublishVolume", zap.Reflect("Request", *req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "UnpublishVolume")
}

// ValidateVolumeCapabilities ...
func (csiCS *CSIControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	ctx = context.WithValue(ctx, provider.RequestID, requestID)
	ctxLogger.Info("CSIControllerServer-ValidateVolumeCapabilities", zap.Reflect("Request", *req))

	// Validate Arguments
	if req.GetVolumeCapabilities() == nil || len(req.GetVolumeCapabilities()) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.NoVolumeCapabilities, requestID, nil)
	}
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.EmptyVolumeID, requestID, nil)
	}

	//Volume ID is in format volumeID:volumeAccessPointID
	tokens := strings.Split(volumeID, ":")
	if len(tokens) != 2 {
		ctxLogger.Info("CSIControllerServer-ValidateVolumeCapabilities...", zap.Reflect("Volume ID is not in format volumeID:accesspointID", tokens))
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, nil)
	}

	// Check if Requested Volume exists
	session, err := csiCS.CSIProvider.GetProviderSession(ctx, ctxLogger)
	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
	}

	// Get volume details by using volume ID, it should exists with provider
	_, err = session.GetVolume(tokens[0])
	if err != nil {
		if providerError.RetrivalFailed == providerError.GetErrorType(err) {
			return nil, commonError.GetCSIError(ctxLogger, commonError.ObjectNotFound, requestID, err, volumeID)
		}
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
	}

	// Setup Response
	var confirmed *csi.ValidateVolumeCapabilitiesResponse_Confirmed
	// Check if Volume Capabilities supported by the Driver Match
	if areVolumeCapabilitiesSupported(req.GetVolumeCapabilities(), csiCS.Driver.vcap) {
		confirmed = &csi.ValidateVolumeCapabilitiesResponse_Confirmed{VolumeCapabilities: req.GetVolumeCapabilities()}
	}

	// Return Response
	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: confirmed,
	}, nil
}

// ListVolumes ...
func (csiCS *CSIControllerServer) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	ctx = context.WithValue(ctx, provider.RequestID, requestID)
	ctxLogger.Info("CSIControllerServer-ListVolumes...", zap.Reflect("Request", *req))
	defer metrics.UpdateDurationFromStart(ctxLogger, metrics.FunctionLabel("ListVolumes"), time.Now())

	session, err := csiCS.CSIProvider.GetProviderSession(ctx, ctxLogger)
	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
	}

	maxEntries := int(req.MaxEntries)
	tags := map[string]string{}
	volumeList, err := session.ListVolumes(maxEntries, req.StartingToken, tags)
	if err != nil {
		errCode := err.(providerError.Message).Code
		if strings.Contains(errCode, "InvalidListVolumesLimit") {
			return nil, commonError.GetCSIError(ctxLogger, commonError.InvalidParameters, requestID, err)
		} else if strings.Contains(errCode, "StartVolumeIDNotFound") {
			return nil, commonError.GetCSIError(ctxLogger, commonError.StartVolumeIDNotFound, requestID, err, req.StartingToken)
		}
		return nil, commonError.GetCSIError(ctxLogger, commonError.ListVolumesFailed, requestID, err)
	}

	entries := []*csi.ListVolumesResponse_Entry{}
	for _, vol := range volumeList.Volumes {
		if vol.Capacity != nil {
			entries = append(entries, &csi.ListVolumesResponse_Entry{
				Volume: &csi.Volume{
					VolumeId:      vol.VolumeID,
					CapacityBytes: int64(*vol.Capacity * utils.GiB),
				},
			})
		}
	}

	return &csi.ListVolumesResponse{
		Entries:   entries,
		NextToken: volumeList.Next,
	}, nil
}

// GetCapacity ...
func (csiCS *CSIControllerServer) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)

	ctxLogger.Info("CSIControllerServer-GetCapacity", zap.Reflect("Request", *req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "GetCapacity")
}

// ControllerGetCapabilities implements the default GRPC callout.
func (csiCS *CSIControllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)

	ctxLogger.Info("CSIControllerServer-ControllerGetCapabilities", zap.Reflect("Request", *req))
	// Return the capabilities as per provider volume capabilities
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: csiCS.Driver.cscap,
	}, nil
}

// CreateSnapshot ...
func (csiCS *CSIControllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)

	ctxLogger.Info("CSIControllerServer-CreateSnapshot", zap.Reflect("Request", *req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "CreateSnapshot")
}

// DeleteSnapshot ...
func (csiCS *CSIControllerServer) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)

	ctxLogger.Info("CSIControllerServer-DeleteSnapshot", zap.Reflect("Request", *req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "DeleteSnapshot")
}

// ListSnapshots ...
func (csiCS *CSIControllerServer) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)

	ctxLogger.Info("CSIControllerServer-ListSnapshots", zap.Reflect("Request", *req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "ListSnapshots")
}

// ControllerExpandVolume ...
func (csiCS *CSIControllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)

	ctxLogger.Info("CSIControllerServer-ControllerExpandVolume", zap.Reflect("Request", requestID))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "ControllerExpandVolume")
}

// ControllerGetVolume ...
func (csiCS *CSIControllerServer) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "ControllerGetVolume")
}
