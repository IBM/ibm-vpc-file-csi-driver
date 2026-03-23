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

	"time"

	"context"

	commonError "github.com/IBM/ibm-csi-common/pkg/messages"
	"github.com/IBM/ibm-csi-common/pkg/metrics"
	"github.com/IBM/ibm-csi-common/pkg/mountmanager"
	"github.com/IBM/ibm-csi-common/pkg/utils"
	"github.com/IBM/ibm-vpc-file-csi-driver/pkg/tunnel"
	nodeMetadata "github.com/IBM/ibmcloud-volume-file-vpc/pkg/metadata"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
	"k8s.io/kubernetes/pkg/volume/util/fs"
	mount "k8s.io/mount-utils"
)

// CSINodeServer ...
type CSINodeServer struct {
	Driver        *IBMCSIDriver
	Mounter       mountmanager.Mounter
	Metadata      nodeMetadata.NodeMetadata
	Stats         StatsUtils
	TunnelManager *tunnel.Manager
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
)

// splitNFSSource splits an NFS source string into server and export path
// Input format: <nfs_server>:/<export_path>
// Returns: [nfs_server, /<export_path>]
func splitNFSSource(source string) []string {
	// Find the first colon which separates server from path
	colonIndex := -1
	for i, c := range source {
		if c == ':' {
			colonIndex = i
			break
		}
	}

	if colonIndex == -1 {
		// No colon found, return source as server with empty path
		return []string{source, "/"}
	}

	server := source[:colonIndex]
	exportPath := source[colonIndex+1:]

	// Ensure export path starts with /
	if len(exportPath) == 0 || exportPath[0] != '/' {
		exportPath = "/" + exportPath
	}

	return []string{server, exportPath}
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
	profileName := req.GetVolumeContext()[ProfileLabel]
	fileShareID := req.GetVolumeContext()[FileShareIDLabel]

	// find  FS type
	fsType := defaultFsType
	// In case EIT is enabled, use eitFsType
	isEITEnabled := req.GetVolumeContext()[IsEITEnabled]
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
	if profileName == RFSProfile && isEITEnabled == TrueStr && transitEncryption == STUNNEL {
		ctxLogger.Info("Setting up Stunnel for RFS volume",
			zap.String("volumeID", volumeID),
			zap.String("nfsServer", source))

		// Initialize tunnel manager if not already done
		if csiNS.TunnelManager == nil {
			tunnelCfg := tunnel.GetConfigFromEnv(ctxLogger)
			var err error
			csiNS.TunnelManager, err = tunnel.NewManager(tunnelCfg)
			if err != nil {
				ctxLogger.Error("Failed to initialize tunnel manager", zap.Error(err))
				return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
			}

			// After creating tunnel manager
			if err := csiNS.TunnelManager.RecoverFromCrash(); err != nil {
				ctxLogger.Warn("Failed to recover tunnels", zap.Error(err))
				// Don't fail startup - recovery is best-effort
			}
		}

		// Parse the NFS server and export path from source
		// Source format: <nfs_server>:/<export_path>
		parts := splitNFSSource(source)
		nfsServer := parts[0]
		exportPath = parts[1]

		// Ensure tunnel exists for this volume
		tun, err := csiNS.TunnelManager.EnsureTunnel(fileShareID, nfsServer)
		if err != nil {
			ctxLogger.Error("Failed to ensure tunnel for volume",
				zap.String("volumeID", volumeID),
				zap.Error(err))
			return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
		}

		// Update mount source to use local tunnel endpoint with export path
		// Format: 127.0.0.1:/<export_path>
		mountSource = fmt.Sprintf("127.0.0.1:%s", exportPath)
		ctxLogger.Info("Tunnel established, using local endpoint",
			zap.String("volumeID", volumeID),
			zap.String("localEndpoint", mountSource),
			zap.String("remoteServer", nfsServer),
			zap.String("exportPath", exportPath),
			zap.Int("tunnelPort", tun.LocalPort))

		// For NFS4 mount through tunnel, use nfs4 fsType and add required options
		fsType = "nfs4"
		// Add NFS4 specific options: vers=4.1, proto=tcp, port=<tunnel_port>
		options = append(options, fmt.Sprintf("vers=4.1,proto=tcp,port=%d", tun.LocalPort))
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
	ctxLogger.Info("Successfully unmounted target path", zap.String("targetPath", targetPath))

	// Initialize tunnel manager this case will occure if CSI driver crash happens
	if csiNS.TunnelManager == nil {
		tunnelCfg := tunnel.GetConfigFromEnv(ctxLogger)
		var err error
		csiNS.TunnelManager, err = tunnel.NewManager(tunnelCfg)
		if err != nil {
			ctxLogger.Error("Failed to initialize tunnel manager", zap.Error(err))
			return nil, commonError.GetCSIError(ctxLogger, commonError.InternalError, requestID, err)
		}
		// After creating tunnel manager
		if err := csiNS.TunnelManager.RecoverFromCrash(); err != nil {
			ctxLogger.Warn("Failed to recover tunnels", zap.Error(err))
			// Don't fail startup - recovery is best-effort
		}
	}

	// Clean up tunnel if it exists for this volume
	// Note: We only remove the tunnel after successful unmount to avoid disrupting active mounts
	if csiNS.TunnelManager != nil {
		// Extract the share ID from the volume ID (format: shareID#clusterID)
		fileShareID := getTokens(volID)
		if len(fileShareID) > 0 {
			shareID := fileShareID[0]

			ctxLogger.Info("Checking for tunnel cleanup",
				zap.String("volumeID", volID),
				zap.String("shareID", shareID))

			if tunnel, exists := csiNS.TunnelManager.GetTunnel(shareID); exists {
				ctxLogger.Info("Found tunnel for volume, attempting removal",
					zap.String("shareID", shareID),
					zap.Int("localPort", tunnel.LocalPort))

				if err := csiNS.TunnelManager.RemoveTunnel(shareID); err != nil {
					ctxLogger.Warn("Failed to remove tunnel, but unmount succeeded",
						zap.String("shareID", shareID),
						zap.Error(err))
					// Don't fail the unmount operation if tunnel cleanup fails
				} else {
					ctxLogger.Info("Tunnel removed successfully", zap.String("shareID", shareID))
				}
			} else {
				ctxLogger.Info("No tunnel found for volume (may not be RFS or already cleaned up)",
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
