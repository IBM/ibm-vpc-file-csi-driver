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
	"net"
	"os"
	"regexp"
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

// splitNFSSource splits an NFS source string into server and export path with comprehensive validation
// Input format: <nfs_server>:/<export_path>
// Returns: NFSSource struct with Server and ExportPath fields, or error if invalid
func splitNFSSource(source string) (*NFSSource, error) {
	if source == "" {
		return nil, fmt.Errorf("NFS source cannot be empty")
	}

	// Length validation to prevent DoS
	const maxNFSSourceLength = 4096 // Reasonable limit for NFS source string
	if len(source) > maxNFSSourceLength {
		return nil, fmt.Errorf("NFS source exceeds maximum length of %d characters", maxNFSSourceLength)
	}

	// Find the first colon which separates server from path
	colonIndex := strings.Index(source, ":")
	if colonIndex == -1 {
		return nil, fmt.Errorf("invalid NFS source format: missing ':' separator (expected format: server:/path)")
	}

	server := source[:colonIndex]
	exportPath := source[colonIndex+1:]

	// Validate server is non-empty and within reasonable length
	const maxServerLength = 253 // RFC 1035 max hostname length
	if server == "" {
		return nil, fmt.Errorf("NFS server cannot be empty")
	}
	if len(server) > maxServerLength {
		return nil, fmt.Errorf("NFS server exceeds maximum length of %d characters", maxServerLength)
	}

	// Validate server format (hostname or IP address)
	if err := validateNFSServer(server); err != nil {
		return nil, fmt.Errorf("invalid NFS server: %w", err)
	}

	// Validate export path format
	const maxPathLength = 4096 // Linux PATH_MAX
	if exportPath == "" {
		return nil, fmt.Errorf("NFS export path cannot be empty")
	}
	if len(exportPath) > maxPathLength {
		return nil, fmt.Errorf("NFS export path exceeds maximum length of %d characters", maxPathLength)
	}
	if exportPath[0] != '/' {
		return nil, fmt.Errorf("NFS export path must start with '/' (got: %s)", exportPath)
	}

	// Security validations for export path
	if err := validateExportPath(exportPath); err != nil {
		return nil, fmt.Errorf("invalid NFS export path: %w", err)
	}

	return &NFSSource{
		Server:     server,
		ExportPath: exportPath,
	}, nil
}

// validHostnameRegex validates RFC 1123 compliant hostnames
// Allows alphanumeric characters, hyphens, and dots
var validHostnameRegex = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`)

// validateNFSServer validates that the server is a valid hostname or IP address
func validateNFSServer(server string) error {
	// Try parsing as IP address first
	if ip := net.ParseIP(server); ip != nil {
		return nil // Valid IP address
	}

	// Validate as hostname (RFC 1123)
	if !validHostnameRegex.MatchString(server) {
		return fmt.Errorf("invalid hostname format: %s (must be valid hostname or IP address)", server)
	}

	return nil
}

// validateExportPath performs comprehensive security validation on the export path
func validateExportPath(path string) error {
	// Check for null bytes (path truncation attack)
	if strings.Contains(path, "\x00") {
		return fmt.Errorf("path contains null bytes")
	}

	// Check for path traversal patterns
	if strings.Contains(path, "/../") || strings.HasSuffix(path, "/..") {
		return fmt.Errorf("path contains parent directory references (..)")
	}
	if strings.Contains(path, "/./") || strings.HasSuffix(path, "/.") {
		return fmt.Errorf("path contains current directory references (.)")
	}

	// Check for double slashes (can cause issues with some NFS implementations)
	if strings.Contains(path, "//") {
		return fmt.Errorf("path contains double slashes")
	}

	// Check for control characters (ASCII 0-31 and 127)
	for i, r := range path {
		if r < 32 || r == 127 {
			return fmt.Errorf("path contains control character at position %d", i)
		}
	}

	// Validate each path component length (max 255 per component for most filesystems)
	const maxComponentLength = 255
	components := strings.Split(path, "/")
	for i, comp := range components {
		if comp == "" {
			continue // Skip empty components (from leading/trailing slashes)
		}
		if len(comp) > maxComponentLength {
			return fmt.Errorf("path component %d exceeds maximum length of %d characters: %s", i, maxComponentLength, comp)
		}
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

		// Set fsType to nfs4 for stunnel
		fsType = nfs4FsType

		if csiNS.StunnelMgr == nil {
			err := fmt.Errorf("stunnel manager is not initialized - this indicates a configuration error. Troubleshooting steps: 1) Check node server pod logs for stunnel initialization errors, 2) Verify OS_TYPE environment variable is set correctly (RHCOS/RHEL/Ubuntu), 3) Verify CLUSTER_ENV is set (production/staging), 4) Ensure CA bundle file exists at expected path, 5) Restart the node server pod to retry initialization")
			ctxLogger.Error("Stunnel manager not available for RFS EIT mount - initialization failed at startup",
				zap.String("volumeID", volumeID),
				zap.String("profileName", profileName),
				zap.String("action", "Check node server pod logs and restart pod after fixing configuration"),
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

		// Ensure tunnel config exists for this volume (stunnel auto-loads it)
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

			// RemoveTunnel is idempotent and handles race conditions internally
			// It will return nil if tunnel doesn't exist or was already removed
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
			ctxLogger.Error("Invalid volume ID format, cannot extract share ID",
				zap.String("volumeID", volID))
			// Don't fail unmount - volume is already unmounted
			// Just log the error and continue
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
