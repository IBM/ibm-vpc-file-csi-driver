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

// Package ibmcsidriver ...
package ibmcsidriver

import (
	"os"
	"strings"
	"time"

	commonError "github.com/IBM/ibm-csi-common/pkg/messages"
	"github.com/IBM/ibm-csi-common/pkg/metrics"
	"github.com/IBM/ibm-csi-common/pkg/utils"
	cloudProvider "github.com/IBM/ibmcloud-volume-file-vpc/pkg/ibmcloudprovider"
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
	// PublishInfoRequestID ...
	PublishInfoRequestID = "request-id"
)

var _ csi.ControllerServer = &CSIControllerServer{}

// ControllerGetCapabilities allows kubernetes to check the supported capabilities of controller service provided by the Plugin
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

// CreateVolume ...
/* CreateVolume is responsible for creating file share and file share-targets.
It takes the csi createVolumeRequest as input and populates the provider volume. It then creates a provider session to invoke the CreateVolume first and
then CreateVolumeAccessPoint call from provider-library. The function returns a csi CreateVolumeResponse if successful and error otherwise.
*/
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

	volumeAccesspointReq := provider.VolumeAccessPointRequest{
		VPCID:             os.Getenv("VPC_ID"),
		AccessControlMode: requestedVolume.AccessControlMode,
	}

	/*
		//IF ENI/VNI is enabled

		Case 1: User has not provided anything.
		The VolumeAccessPoint (aka File share target) will be created with mountPath having randomIP from subnet within the same zone as used by the
		volume (aka File share). The zone is picked-up randomly from topology and the subnet is fetched via the CSI driver matching with the
		volumeaccess point zone and cluster subnet list.In this case any random IP Address will be created and assigned to VNI in the fetched subnet range.

		Case 2: User has provided the subnetId, zone but nothing else
		The VolumeAccessPoint (aka File share target) will be created with mountPath having randomIP from subnet within the zone provided by user
		as used by the volume (aka File share).In this case any random IP Address will be created and assigned to VNI in the user provided subnet range.

		Case 3: User has provided the subnetId, zone and PrimaryIPAddress
		The VolumeAccessPoint (aka File share target) will be created with mountPath having PrimaryIPAddress from subnet within the zone provided by user
		as the volume (aka File share).In this case any PrimaryIPAddress will be created and assigned to VNI in the user provided subnet range.

		Case 4: User has provided the subnetId, zone and PrimaryIPID
		The VolumeAccessPoint (aka File share target) will be created with mountPath having IP adress associated with PrimaryIPID from subnet within
		the zone provided by user as used by the volume (aka File share).In this case any IP adress associated with PrimaryIPID is assigned to VNI.

		Case 5: User has not provided the subnetId and provided zone, PrimaryIPID
		The VolumeAccessPoint (aka File share target) will be created with mountPath having IP adress associated with PrimaryIPID from subnet within
		the zone provided by user as used by the volume (aka File share).In this case any IP adress associated with PrimaryIPID is assigned to VNI.

		Case 6: User has not provided the subnetId,zone but provided PrimaryIPID
		This will throw error that zone is mandatory as CSI driver cannot predict the zone in such scenarios CSI will pick this up from topology.

		Case 7: User has provided the subnetId but nothing else
		This will throw error that zone is mandatory as CSI driver cannot predict the zone in such scenarios CSI will pick this up from topology.

		Case 8: User has not provided the subnetID but provided the zone and PrimaryIPAddress
		This will throw error that subnet is mandatory as CSI driver cannot predict the subnet in such scenarios as there
		might be multiple subnets in same zone.

		Case 9: User has provided the subnetID and PrimaryIPAddress but not provided the zone.
		This will throw error that zone is mandatory as CSI driver cannot predict the zone in such scenarios CSI will pick this up from topology.

		In all the above cases the variation possible is user can pass 0 or more securitygroupIDs that will govern the authorization. IF user does not pass
		any securityGroupID then

		1. IKS cluster security group is fetched and used by CSI driver
		2. Else default VPC security group is considerd by the VPC IAAS layer.

	*/

	if requestedVolume.AccessControlMode == SecurityGroup {
		/* Skip GetSubnetForVolumeAccessPoint call if user has not provided SubnetID but PrimaryIPID is provided.
		For all rest of the following use cases if subnetId is not provided we fetch subnet
		1.) User has not provided anything just ENI/VNI is enabled (Any random IP Address will be created and assigned to VNI in the fetch subnet range)
		2.) User has provided PrimaryIP Address. (The respective IP Address will be created and assigned to VNI in the fetched subnet range)
		*/
		subnetID := requestedVolume.SubnetID

		if len(subnetID) == 0 && (requestedVolume.PrimaryIP == nil || len(requestedVolume.PrimaryIP.ID) == 0) {
			//subnetIDList := os.Getenv("VPC_SUBNET_IDS")
			subnetIDList := VPC_SUBNET_IDS

			//We need to abort here as there is no use of going ahead and fetching the matching subnet with empty list
			if len(subnetIDList) == 0 {
				return nil, commonError.GetCSIError(ctxLogger, commonError.SubnetIDListNotFound, requestID, nil)
			}

			subnetReq := provider.SubnetRequest{
				SubnetIDList:  subnetIDList,
				ZoneName:      requestedVolume.Az,
				VPCID:         os.Getenv("VPC_ID"),
				ResourceGroup: requestedVolume.ResourceGroup,
			}

			ctxLogger.Info("Getting Subnet for VolumeAccessPoint...")

			subnetID, err = session.GetSubnetForVolumeAccessPoint(subnetReq)
			if err != nil || len(subnetID) == 0 {
				return nil, commonError.GetCSIError(ctxLogger, commonError.SubnetFindFailed, requestID, err, requestedVolume.Az, subnetIDList)
			}
			ctxLogger.Info("Subnet fetched for VolumeAccessPoint", zap.Reflect("subnetID", subnetID))
		}

		//If securityGroup parameter is not populated via storage class
		if requestedVolume.SecurityGroups == nil {
			securityGroupReq := provider.SecurityGroupRequest{
				Name:          "kube-" + csiCS.CSIProvider.GetClusterID(),
				VPCID:         os.Getenv("VPC_ID"),
				ResourceGroup: requestedVolume.ResourceGroup,
			}

			ctxLogger.Info("Getting SecurityGroup for VolumeAccessPoint...")

			securityGroupID, err := session.GetSecurityGroupForVolumeAccessPoint(securityGroupReq)
			if err != nil || len(securityGroupID) == 0 {
				// If IKS Cluster SG is not available pass empty SG. VPC IAAS will consider VPC Default SG.
				ctxLogger.Warn("SecurityGroup find failed for VolumeAccessPoint.VPC default SG will be considered", zap.Error(err))
			} else {
				requestedVolume.SecurityGroups = &[]provider.SecurityGroup{
					{
						ID: securityGroupID,
					},
				}
				ctxLogger.Info("SecurityGroup fetched for VolumeAccessPoint", zap.Reflect("securityGroupID", securityGroupID))
			}
		}

		volumeAccesspointReq.ResourceGroup = requestedVolume.ResourceGroup
		volumeAccesspointReq.SecurityGroups = requestedVolume.SecurityGroups
		volumeAccesspointReq.PrimaryIP = requestedVolume.PrimaryIP
		volumeAccesspointReq.SubnetID = subnetID
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

	volumeAccesspointReq.VolumeID = volumeObj.VolumeID

	//Create VolumeAccess Point
	//No need to check for access point existence as library takes care of the same
	volumeAccessPointObj, err := createVolumeAccessPoint(session, volumeAccesspointReq, ctxLogger)

	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
	}

	// return csi volume object
	return createCSIVolumeResponse(*volumeObj, *volumeAccessPointObj, int64(*(requestedVolume.Capacity)*utils.GB), nil, csiCS.CSIProvider.GetClusterID()), nil
}

// DeleteVolume ...
/* DeleteVolume is responsible for deleting file share-targets and file share.
It takes the csi deleteVolumeRequest as input and creates a provider session to invoke DeleteVolumeAccessPoint first and
then DeleteVolume call from provider-library. The function returns a csi DeleteVolumeResponse if successful and error otherwise.
*/
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

	//Volume ID is in format volumeID:volumeAccessPointID, to assist the deletion of volume access point
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

// ValidateVolumeCapabilities ...
/* ValidateVolumeCapabilities is responsible to check if a pre-provisioned volume has all the capabilities that the CO wants.
This RPC call SHALL return confirmed only if all the volume capabilities specified in the request are supported.
*/
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

// ListVolumes is responsible for returning the information about all the volumes that it knows about.
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

// ControllerExpandVolume ...
/* ControllerExpandVolume is responsible for upsizing the file share.
It takes ControllerExpandVolumeRequest as input and creates a provider session to invoke ExpandVolumeRequest cal
from provider-library. The function returns a csi ControllerExpandVolumeResponse if successful and error otherwise.
*/
func (csiCS *CSIControllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)

	ctxLogger.Info("CSIControllerServer-ControllerExpandVolume", zap.Reflect("Request", requestID))
	volumeID := req.GetVolumeId()
	capacity := req.GetCapacityRange().GetRequiredBytes()
	if len(volumeID) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.EmptyVolumeID, requestID, nil)
	}

	// get the session
	session, err := csiCS.CSIProvider.GetProviderSession(ctx, ctxLogger)
	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.FailedPrecondition, requestID, err)
	}
	requestedVolume := &provider.Volume{}

	//Volume ID is in format volumeID:volumeAccessPointID, to assist the deletion of volume access point
	tokens := strings.Split(volumeID, ":")
	if len(tokens) != 2 {
		ctxLogger.Info("CSIControllerServer-ExpandVolume...", zap.Reflect("Volume ID is not in format volumeID:accesspointID", tokens))
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, nil)
	}

	requestedVolume.VolumeID = tokens[0]
	volDetail, err := checkIfVolumeExists(session, *requestedVolume, ctxLogger)

	// Volume not found
	if volDetail == nil && err == nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.ObjectNotFound, requestID, nil, volumeID)
	} else if err != nil { // In case of other errors apart from volume not  found
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
	}

	volumeExpansionReq := provider.ExpandVolumeRequest{
		VolumeID: requestedVolume.VolumeID,
		Capacity: capacity,
	}
	_, err = session.ExpandVolume(volumeExpansionReq)
	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
	}
	return &csi.ControllerExpandVolumeResponse{CapacityBytes: capacity, NodeExpansionRequired: true}, nil
}

// ControllerPublishVolume ...
func (csiCS *CSIControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)

	ctxLogger.Info("CSIControllerServer-ControllerPublishVolume", zap.Reflect("Request", *req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnsupported, requestID, nil, "PublishVolume")
}

// ControllerUnpublishVolume ...
func (csiCS *CSIControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)

	ctxLogger.Info("CSIControllerServer-ControllerUnpublishVolume", zap.Reflect("Request", *req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnsupported, requestID, nil, "UnpublishVolume")
}

// GetCapacity ...
func (csiCS *CSIControllerServer) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)

	ctxLogger.Info("CSIControllerServer-GetCapacity", zap.Reflect("Request", *req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "GetCapacity")
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

// ControllerGetVolume ...
func (csiCS *CSIControllerServer) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	// populate requestID in the context
	_ = context.WithValue(ctx, provider.RequestID, requestID)
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "ControllerGetVolume")
}
