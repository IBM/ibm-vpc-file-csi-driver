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
	"fmt"
	"os"
	"strings"

	"time"

	"context"

	commonError "github.com/IBM/ibm-csi-common/pkg/messages"
	"github.com/IBM/ibm-csi-common/pkg/metrics"
	"github.com/IBM/ibm-csi-common/pkg/mountmanager"
	"github.com/IBM/ibm-csi-common/pkg/utils"
	"github.com/IBM/ibm-vpc-file-csi-driver/pkg/stunnel"
	nodeMetadata "github.com/IBM/ibmcloud-volume-file-vpc/pkg/metadata"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
	"k8s.io/kubernetes/pkg/volume/util/fs"
	mount "k8s.io/mount-utils"
)

// CSINodeServer ...
type CSINodeServer struct {
	Driver     *IBMCSIDriver
	Mounter    mountmanager.Mounter
	Metadata   nodeMetadata.NodeMetadata
	Stats      StatsUtils
	StunnelMgr *stunnel.SimpleManager
	// TODO: Only lock mutually exclusive calls and make locking more fine grained
	mutex utils.LockStore
	csi.UnimplementedNodeServer
}

// StatsUtils ...
type StatsUtils interface {
	FSInfo(path string) (int64, int64, int64, int64, int64, int64, error)
	IsDevicePathNotExist(devicePath string) bool
}

// VolumeMountUtils ...
type VolumeMountUtils struct {
}

// VolumeStatUtils ...
type VolumeStatUtils struct {
}

// FSInfo ...
func (su *VolumeStatUtils) FSInfo(path string) (int64, int64, int64, int64, int64, int64, error) {
	return fs.Info(path)
}

const (
	// default file system type to be used when it is not provided
	defaultFsType = "nfs"
	// file system in case transit encryption is enabled
	eitFsType = "ibmshare"
	// file system type for NFS version 4 (required for stunnel)
	nfs4FsType = "nfs4"
)

// NFSSource represents a parsed NFS source with server and export path
type NFSSource struct {
	Server     string
	ExportPath string
}

// splitNFSSource splits an NFS source string into server and export path
// Input format: <nfs_server>:/<export_path>
// Returns: NFSSource struct with Server and ExportPath fields, or error if invalid
func splitNFSSource(source string) (*NFSSource, error) {
	if source == "" {
		return nil, fmt.Errorf("NFS source cannot be empty")
	}

	// Find the first colon which separates server from path
	colonIndex := strings.Index(source, ":")
	if colonIndex == -1 {
		return nil, fmt.Errorf("invalid NFS source format: missing ':' separator (expected format: server:/path)")
	}

	server := source[:colonIndex]
	exportPath := source[colonIndex+1:]

	// Validate server is non-empty
	if server == "" {
		return nil, fmt.Errorf("NFS server cannot be empty")
	}

	// Validate export path format
	if exportPath == "" {
		return nil, fmt.Errorf("NFS export path cannot be empty")
	}
	if exportPath[0] != '/' {
		return nil, fmt.Errorf("NFS export path must start with '/' (got: %s)", exportPath)
	}
	// Additional validation: ensure path doesn't contain invalid characters or patterns
	if strings.Contains(exportPath, "//") {
		return nil, fmt.Errorf("NFS export path contains invalid double slashes: %s", exportPath)
	}

	return &NFSSource{
		Server:     server,
		ExportPath: exportPath,
	}, nil
}

// validateNFSMountOptions validates that required NFS mount options are present for stunnel
// Returns error if required options (vers, proto) are missing
func validateNFSMountOptions(options []string) error {
	if len(options) == 0 {
		return fmt.Errorf("mount options are required for RFS with stunnel (must include 'vers' and 'proto')")
	}

	hasVers := false
	hasProto := false

	for _, opt := range options {
		if strings.HasPrefix(opt, "vers=") || strings.HasPrefix(opt, "nfsvers=") {
			hasVers = true
		}
		if strings.HasPrefix(opt, "proto=") {
			hasProto = true
		}
	}

	var missingOpts []string
	if !hasVers {
		missingOpts = append(missingOpts, "vers")
	}
	if !hasProto {
		missingOpts = append(missingOpts, "proto")
	}

	if len(missingOpts) > 0 {
		return fmt.Errorf("missing required mount options for RFS with stunnel: %v. Storage class must include these in mountOptions", missingOpts)
	}

	return nil
}

var _ csi.NodeServer = &CSINodeServer{}

// NodePublishVolume ...
func (csiNS *CSINodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodePublishVolume...", zap.Reflect("Request", req))
	defer metrics.UpdateDurationFromStart(ctxLogger, "NodePublishVolume", time.Now())

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.EmptyVolumeID, requestID, nil)
	}

	source := req.GetVolumeContext()[NFSServerPath]
	if len(source) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.NoStagingTargetPath, requestID, nil)
	}

	target := req.GetTargetPath()
	if len(target) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.NoTargetPath, requestID, nil)
	}

	volumeCapability := req.GetVolumeCapability()
	if volumeCapability == nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.NoVolumeCapabilities, requestID, nil)
	}

	volumeCapabilities := []*csi.VolumeCapability{volumeCapability}
	// Validate volume capabilities, are all capabilities supported by driver or not
	if !areVolumeCapabilitiesSupported(volumeCapabilities, csiNS.Driver.vcap) {
		return nil, commonError.GetCSIError(ctxLogger, commonError.VolumeCapabilitiesNotSupported, requestID, nil)
	}

	// Check if targetPath is already mounted. If it already moounted return OK
	notMounted, err := csiNS.Mounter.IsLikelyNotMountPoint(target)
	if err != nil && !os.IsNotExist(err) {
		//Error other than PathNotExists
		ctxLogger.Error(fmt.Sprintf("Can not validate target mount point: %s %v", target, err))
		return nil, commonError.GetCSIError(ctxLogger, commonError.MountPointValidateError, requestID, err, target)
	}
	// Its OK if IsLikelyNotMountPoint returns PathNotExists error
	if !notMounted {
		// The  target Path is already mounted, Retrun OK
		/* TODO
		1) Target Path MUST be the vol referenced by vol ID
		2) Check volume capability matches for ALREADY_EXISTS
		3) Readonly MUST match
		*/
		ctxLogger.Warn("target Path is already mounted")
		return &csi.NodePublishVolumeResponse{}, nil
	}
	mnt := volumeCapability.GetMount()
	options := mnt.MountFlags
	transitEncryption := STUNNEL
	// Get volume context
	volumeContext := req.GetVolumeContext()

	// Get profile name from volume context (optional)
	profileName := volumeContext[ProfileLabel]
	fileShareID := volumeContext[FileShareIDLabel]
	isEITEnabled := volumeContext[IsEITEnabled]

	// find  FS type
	fsType := defaultFsType
	// In case EIT is enabled, use eitFsType
	if isEITEnabled == TrueStr {
		if profileName == DP2Profile {
			transitEncryption = IPSEC
			fsType = eitFsType
		}
	}

	var nodePublishResponse *csi.NodePublishVolumeResponse
	var mountErr error

	//Lets try to put lock at targetPath level. If we are processing same target path lets wait for other to finish.
	//This will not hold other volumes and target path processing.
	csiNS.mutex.Lock(target)
	defer csiNS.mutex.Unlock(target)

	// Handle RFS profile with Stunnel encryption
	mountSource := source
	var exportPath string
	if profileName == RFSProfile && isEITEnabled == TrueStr {
		ctxLogger.Info("Setting up Stunnel for RFS volume",
			zap.String("volumeID", volumeID),
			zap.String("nfsServer", source))

		// Validate fsType for RFS with stunnel - must be nfs4 if provided
		if mnt.FsType != "" && mnt.FsType != nfs4FsType {
			err := fmt.Errorf("invalid fsType '%s' for RFS profile with encryption-in-transit, must be '%s'", mnt.FsType, nfs4FsType)
			ctxLogger.Error("Invalid fsType for RFS with stunnel",
				zap.String("volumeID", volumeID),
				zap.String("fsType", mnt.FsType),
				zap.Error(err))
			return nil, commonError.GetCSIError(ctxLogger, commonError.InvalidParameters, requestID, err)
		}
		// Set fsType to nfs4 for stunnel
		fsType = nfs4FsType

		// Validate that storage class provides required NFS mount options
		if err := validateNFSMountOptions(options); err != nil {
			ctxLogger.Error("Invalid mount options for RFS with stunnel",
				zap.String("volumeID", volumeID),
				zap.Strings("options", options),
				zap.Error(err))
			return nil, commonError.GetCSIError(ctxLogger, commonError.InvalidParameters, requestID, err)
		}

		if csiNS.StunnelMgr == nil {
			err := fmt.Errorf("stunnel manager is not configured, please restart the node server which will try to initialize the stunnel manager")
			ctxLogger.Error("Failed to ensure tunnel for volume",
				zap.String("volumeID", volumeID),
				zap.Error(err))
			return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
		}

		// Parse the NFS server and export path from source
		// Source format: <nfs_server>:/<export_path>
		nfsSource, err := splitNFSSource(source)
		if err != nil {
			ctxLogger.Error("Failed to parse NFS source",
				zap.String("source", source),
				zap.Error(err))
			return nil, commonError.GetCSIError(ctxLogger, commonError.InvalidParameters, requestID, err)
		}
		nfsServer := nfsSource.Server
		exportPath = nfsSource.ExportPath

		// Ensure tunnel config exists for this volume (denali-stunnel auto-loads it)
		tunnelPort, err := csiNS.StunnelMgr.EnsureTunnel(fileShareID, nfsServer, requestID)
		if err != nil {
			ctxLogger.Error("Failed to create tunnel config for volume",
				zap.String("volumeID", volumeID),
				zap.Error(err))
			return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
		}

		// Update mount source to use local tunnel endpoint with export path
		// Format: 127.0.0.1:/<export_path>
		mountSource = fmt.Sprintf("127.0.0.1:%s", exportPath)

		// Append port to existing mount options (storage class provides vers, proto, etc.)
		// Storage class must include mount options like: vers=4.1,proto=tcp
		options = append(options, fmt.Sprintf("port=%d", tunnelPort))
	}

	nodePublishResponse, mountErr = csiNS.processMount(ctxLogger, requestID, mountSource, target, fsType, transitEncryption, options)

	ctxLogger.Info("CSINodeServer-NodePublishVolume response...", zap.Reflect("Response", nodePublishResponse), zap.Error(mountErr))
	return nodePublishResponse, mountErr
}

// NodeUnpublishVolume ...
func (csiNS *CSINodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeUnpublishVolume...", zap.Reflect("Request", req))
	defer metrics.UpdateDurationFromStart(ctxLogger, "NodeUnpublishVolume", time.Now())

	// Validate Arguments
	targetPath := req.GetTargetPath()
	volID := req.GetVolumeId()
	if len(volID) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.EmptyVolumeID, requestID, nil)
	}
	if len(targetPath) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.NoTargetPath, requestID, nil)
	}

	//Lets try to put lock at targetPath level. If we are processing same target path lets wait for other to finish.
	//This will not hold other volumes and target path processing.
	csiNS.mutex.Lock(targetPath)
	defer csiNS.mutex.Unlock(targetPath)

	ctxLogger.Info("Unmounting target path", zap.String("targetPath", targetPath))
	err := mount.CleanupMountPoint(targetPath, csiNS.Mounter, false /* bind mount */)
	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.UnmountFailed, requestID, err, targetPath)
	}

	// Clean up tunnel config if it exists for this volume
	// Note: We only remove the tunnel after successful unmount to avoid disrupting active mounts
	if csiNS.StunnelMgr != nil {
		// Extract the share ID from the volume ID (format: shareID#targetID)
		fileShareID := getTokens(volID)
		if len(fileShareID) > 0 {
			shareID := fileShareID[0]

			ctxLogger.Info("Checking for tunnel config cleanup",
				zap.String("volumeID", volID),
				zap.String("shareID", shareID))

			// Check if tunnel config exists
			if port, exists := csiNS.StunnelMgr.GetTunnelPort(shareID); exists {
				ctxLogger.Info("Found tunnel config for volume, attempting removal",
					zap.String("shareID", shareID),
					zap.Int("port", port))

				if err := csiNS.StunnelMgr.RemoveTunnel(shareID, requestID); err != nil {
					ctxLogger.Error("Failed to remove tunnel config after unmount, will trigger retry",
						zap.String("shareID", shareID),
						zap.Error(err))
					// Return error to trigger Kubernetes retry
					// The rollback logic in RemoveTunnel ensures port maps stay consistent
					// K8s will retry NodeUnpublishVolume, which will:
					// 1. Try to unmount again (will succeed as already unmounted or be idempotent)
					// 2. Retry tunnel cleanup until it succeeds
					return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
				}
				ctxLogger.Info("Tunnel config removed successfully", zap.String("shareID", shareID))
			} else {
				ctxLogger.Info("No tunnel config found for volume (may not be RFS or already cleaned up)",
					zap.String("shareID", shareID))
			}
		}
	}

	nodeUnpublishVolumeResponse := &csi.NodeUnpublishVolumeResponse{}
	ctxLogger.Info("Successfully unmounted target path", zap.String("targetPath", targetPath))
	return nodeUnpublishVolumeResponse, nil
}

// NodeStageVolume ...
func (csiNS *CSINodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeStageVolume", zap.Reflect("Request", req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnsupported, requestID, nil, "NodeStageVolume")
}

// NodeUnstageVolume ...
func (csiNS *CSINodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeUnstageVolume", zap.Reflect("Request", req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnsupported, requestID, nil, "NodeUnstageVolume")
}

// NodeGetCapabilities ...
func (csiNS *CSINodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	ctxLogger, _ := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeGetCapabilities... ", zap.Reflect("Request", req))

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: csiNS.Driver.nscap,
	}, nil
}

// NodeGetInfo ...
func (csiNS *CSINodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeGetInfo... ", zap.Reflect("Request", req))

	// Check if node metadata service initialized properly
	if csiNS.Metadata == nil { //nolint
		nodeName := os.Getenv("KUBE_NODE_NAME")

		nodeInfo := nodeMetadata.NodeInfoManager{
			NodeName: nodeName,
		}

		metadata, err := nodeInfo.NewNodeMetadata(ctxLogger)
		if err != nil {
			ctxLogger.Error("Failed to initialize node metadata", zap.Error(err))
			return nil, commonError.GetCSIError(ctxLogger, commonError.NodeMetadataInitFailed, requestID, err)
		}
		csiNS.Metadata = metadata
	}

	top := &csi.Topology{
		Segments: map[string]string{
			utils.NodeRegionLabel: csiNS.Metadata.GetRegion(),
			utils.NodeZoneLabel:   csiNS.Metadata.GetZone(),
		},
	}

	resp := &csi.NodeGetInfoResponse{
		NodeId:             csiNS.Metadata.GetWorkerID(),
		AccessibleTopology: top,
	}
	ctxLogger.Info("NodeGetInfoResponse", zap.Reflect("NodeGetInfoResponse", resp))
	return resp, nil
}

// NodeGetVolumeStats ...
func (csiNS *CSINodeServer) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	var resp *csi.NodeGetVolumeStatsResponse
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeGetVolumeStats... ", zap.Reflect("Request", req))
	defer metrics.UpdateDurationFromStart(ctxLogger, "NodeGetVolumeStats", time.Now())
	if req == nil || req.VolumeId == "" { //nolint
		return nil, commonError.GetCSIError(ctxLogger, commonError.EmptyVolumeID, requestID, nil)
	}

	if req.VolumePath == "" {
		return nil, commonError.GetCSIError(ctxLogger, commonError.EmptyVolumePath, requestID, nil)
	}

	volumePath := req.VolumePath
	// Return if path does not exist
	if csiNS.Stats.IsDevicePathNotExist(volumePath) {
		return nil, commonError.GetCSIError(ctxLogger, commonError.DevicePathNotExists, requestID, nil, volumePath, req.VolumeId)
	}

	// else get the file system stats
	available, capacity, usage, inodes, inodesFree, inodesUsed, err := csiNS.Stats.FSInfo(volumePath)
	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.GetFSInfoFailed, requestID, err)
	}
	resp = &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: available,
				Total:     capacity,
				Used:      usage,
				Unit:      csi.VolumeUsage_BYTES,
			},
			{
				Available: inodesFree,
				Total:     inodes,
				Used:      inodesUsed,
				Unit:      csi.VolumeUsage_INODES,
			},
		},
	}

	ctxLogger.Info("Response for Volume stats", zap.Reflect("Response", resp))
	return resp, nil
}

// NodeExpandVolume ...
func (csiNS *CSINodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeExpandVolume", zap.Reflect("Request", req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnsupported, requestID, nil, "NodeExpandVolume")
}

// IsDevicePathNotExist ...
func (su *VolumeStatUtils) IsDevicePathNotExist(devicePath string) bool {
	var stat unix.Stat_t
	err := unix.Stat(devicePath, &stat)
	if err != nil {
		if os.IsNotExist(err) {
			return true
		}
	}
	return false
}
