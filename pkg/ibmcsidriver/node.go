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
	"fmt"
	"os"
	"runtime"
	"sync"

	"time"

	commonError "github.com/IBM/ibm-csi-common/pkg/messages"
	nodeMetadata "github.com/IBM/ibm-csi-common/pkg/metadata"
	"github.com/IBM/ibm-csi-common/pkg/metrics"
	"github.com/IBM/ibm-csi-common/pkg/utils"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"go.uber.org/zap"
	"golang.org/x/net/context"
	"golang.org/x/sys/unix"
	"k8s.io/kubernetes/pkg/util/mount"
	"k8s.io/kubernetes/pkg/volume/util/fs"
)

// CSINodeServer ...
type CSINodeServer struct {
	Driver   *IBMCSIDriver
	Mounter  mount.Interface
	Metadata nodeMetadata.NodeMetadata
	Stats    StatsUtils
	// TODO: Only lock mutually exclusive calls and make locking more fine grained
	mux sync.Mutex
}

// StatsUtils ...
type StatsUtils interface {
	FSInfo(path string) (int64, int64, int64, int64, int64, int64, error)
	IsDevicePathNotExist(devicePath string) bool
}

// VolumeStatUtils ...
type VolumeStatUtils struct {
}

//FSInfo ...
func (su *VolumeStatUtils) FSInfo(path string) (int64, int64, int64, int64, int64, int64, error) {
	return fs.FsInfo(path)
}

const (
	// DefaultVolumesPerNode is the default number of volumes attachable to a node
	DefaultVolumesPerNode = 4

	// MaxVolumesPerNode is the maximum number of volumes attachable to a node
	MaxVolumesPerNode = 12

	// MinimumCoresWithMaximumAttachableVolumes is the minimum cores required to have maximum number of attachable volumes, currently 4 as per the docs.
	MinimumCoresWithMaximumAttachableVolumes = 4

	// FSTypeExt2 represents the ext2 filesystem type
	FSTypeExt2 = "ext2"

	// FSTypeExt3 represents the ext3 filesystem type
	FSTypeExt3 = "ext3"

	// FSTypeExt4 represents the ext4 filesystem type
	FSTypeExt4 = "ext4"

	// FSTypeXfs represents te xfs filesystem type
	FSTypeXfs = "xfs"

	// default file system type to be used when it is not provided
	defaultFsType = "nfs"
)

var _ csi.NodeServer = &CSINodeServer{}

// NodePublishVolume ...
func (csiNS *CSINodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	publishContext := req.GetPublishContext()
	controlleRequestID := publishContext[PublishInfoRequestID]
	ctxLogger, requestID := utils.GetContextLoggerWithRequestID(ctx, false, &controlleRequestID)
	ctxLogger.Info("CSINodeServer-NodePublishVolume...", zap.Reflect("Request", *req))
	metrics.UpdateDurationFromStart(ctxLogger, "NodePublishVolume", time.Now())

	csiNS.mux.Lock()
	defer csiNS.mux.Unlock()

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
		return &csi.NodePublishVolumeResponse{}, nil
	}
	mnt := volumeCapability.GetMount()
	options := mnt.MountFlags
	// find  FS type
	fsType := defaultFsType
	if mnt.FsType != "" {
		fsType = mnt.FsType
	}

	var nodePublishResponse *csi.NodePublishVolumeResponse
	var mountErr error

	nodePublishResponse, mountErr = csiNS.processMount(ctxLogger, requestID, source, target, fsType, options)

	ctxLogger.Info("CSINodeServer-NodePublishVolume response...", zap.Reflect("Response", nodePublishResponse), zap.Error(mountErr))
	return nodePublishResponse, mountErr
}

// NodeUnpublishVolume ...
func (csiNS *CSINodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeUnpublishVolume...", zap.Reflect("Request", *req))
	metrics.UpdateDurationFromStart(ctxLogger, "NodeUnpublishVolume", time.Now())
	csiNS.mux.Lock()
	defer csiNS.mux.Unlock()
	// Validate Arguments
	targetPath := req.GetTargetPath()
	volID := req.GetVolumeId()
	if len(volID) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.EmptyVolumeID, requestID, nil)
	}
	if len(targetPath) == 0 {
		return nil, commonError.GetCSIError(ctxLogger, commonError.NoTargetPath, requestID, nil)
	}

	ctxLogger.Info("Unmounting  target path", zap.String("targetPath", targetPath))
	err := mount.CleanupMountPoint(targetPath, csiNS.Mounter, false /* bind mount */)
	if err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.UnmountFailed, requestID, err, targetPath)
	}

	nodeUnpublishVolumeResponse := &csi.NodeUnpublishVolumeResponse{}
	ctxLogger.Info("Successfully unmounted  target path", zap.String("targetPath", targetPath), zap.Error(err))
	return nodeUnpublishVolumeResponse, err
}

// NodeStageVolume ...
func (csiNS *CSINodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeStageVolume", zap.Reflect("Request", *req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "NodeStageVolume")
}

// NodeUnstageVolume ...
func (csiNS *CSINodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeUnstageVolume", zap.Reflect("Request", *req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "NodeUnstageVolume")
}

// NodeGetCapabilities ...
func (csiNS *CSINodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	ctxLogger, _ := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeGetCapabilities... ", zap.Reflect("Request", *req))

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: csiNS.Driver.nscap,
	}, nil
}

// NodeGetInfo ...
func (csiNS *CSINodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeGetInfo... ", zap.Reflect("Request", *req))

	// maxVolumesPerNode is the maximum number of volumes attachable to a node
	var maxVolumesPerNode int64 = DefaultVolumesPerNode

	// Check if node metadata service initialized properly
	if csiNS.Metadata == nil {
		metadata, err := nodeMetadata.NewNodeMetadata(os.Getenv("KUBE_NODE_NAME"), ctxLogger)
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

	// maxVolumesPerNode is the maximum number of volumes attachable to a node; default is 4
	cores := runtime.NumCPU()
	if cores >= MinimumCoresWithMaximumAttachableVolumes {
		maxVolumesPerNode = MaxVolumesPerNode
	}
	ctxLogger.Info("Number of cores of the node and attachable volume limits.", zap.Reflect("Cores", cores), zap.Reflect("AttachableVolumeLimits", maxVolumesPerNode))

	resp := &csi.NodeGetInfoResponse{
		NodeId:             csiNS.Metadata.GetWorkerID(),
		MaxVolumesPerNode:  maxVolumesPerNode,
		AccessibleTopology: top,
	}
	ctxLogger.Info("NodeGetInfoResponse", zap.Reflect("NodeGetInfoResponse", resp))
	return resp, nil
}

// NodeGetVolumeStats ...
func (csiNS *CSINodeServer) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	var resp *csi.NodeGetVolumeStatsResponse
	ctxLogger, requestID := utils.GetContextLogger(ctx, false)
	ctxLogger.Info("CSINodeServer-NodeGetVolumeStats... ", zap.Reflect("Request", *req)) //nolint:staticcheck
	metrics.UpdateDurationFromStart(ctxLogger, "NodeGetVolumeStats", time.Now())
	if req == nil || req.VolumeId == "" {
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
	ctxLogger.Info("CSINodeServer-NodeExpandVolume", zap.Reflect("Request", *req))
	return nil, commonError.GetCSIError(ctxLogger, commonError.MethodUnimplemented, requestID, nil, "NodeExpandVolume")
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
