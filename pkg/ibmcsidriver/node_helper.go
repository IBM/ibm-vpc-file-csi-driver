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

	commonError "github.com/IBM/ibm-csi-common/pkg/messages"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"go.uber.org/zap"
)

func (csiNS *CSINodeServer) processMount(ctxLogger *zap.Logger, requestID, stagingTargetPath, targetPath, fsType string, options []string) (*csi.NodePublishVolumeResponse, error) {
	stagingTargetPathField := zap.String("stagingTargetPath", stagingTargetPath)
	targetPathField := zap.String("targetPath", targetPath)
	fsTypeField := zap.String("fsType", fsType)
	optionsField := zap.Reflect("options", options)
	ctxLogger.Info("CSINodeServer-processMount...", stagingTargetPathField, targetPathField, fsTypeField, optionsField)
	if err := csiNS.Mounter.MakeDir(targetPath); err != nil {
		return nil, commonError.GetCSIError(ctxLogger, commonError.TargetPathCreateFailed, requestID, err, targetPath)
	}

	var err error

	ctxLogger.Info("Creating request for mounting volume...")
	if fsType != eitFsType {
		err = csiNS.Mounter.Mount(stagingTargetPath, targetPath, fsType, options)
	} else {
		err = csiNS.Mounter.MountEITBasedFileShare(stagingTargetPath, targetPath, fsType, requestID)
	}

	if err != nil {
		notMnt, mntErr := csiNS.Mounter.IsLikelyNotMountPoint(targetPath)
		if mntErr != nil {
			return nil, commonError.GetCSIError(ctxLogger, commonError.MountPointValidateError, requestID, mntErr, targetPath)
		}
		if !notMnt {
			if mntErr = csiNS.Mounter.Unmount(targetPath); mntErr != nil {
				return nil, commonError.GetCSIError(ctxLogger, commonError.UnmountFailed, requestID, mntErr, targetPath)
			}
			notMnt, mntErr = csiNS.Mounter.IsLikelyNotMountPoint(targetPath)
			if mntErr != nil {
				return nil, commonError.GetCSIError(ctxLogger, commonError.MountPointValidateError, requestID, mntErr, targetPath)
			}
			if !notMnt {
				// This is very odd, we don't expect it.  We'll try again next sync loop.
				return nil, commonError.GetCSIError(ctxLogger, commonError.UnmountFailed, requestID, err, targetPath)
			}
		}
		errRemovePath := os.Remove(targetPath)
		if errRemovePath != nil {
			ctxLogger.Warn("processMount: Remove targePath failed", zap.String("targetPath", targetPath), zap.Error(errRemovePath))
		}
		errorCode := checkMountResponse(err)
		return nil, commonError.GetCSIError(ctxLogger, errorCode, requestID, err)
	}

	ctxLogger.Info("CSINodeServer-processMount successfully mounted", stagingTargetPathField, targetPathField, fsTypeField, optionsField)
	return &csi.NodePublishVolumeResponse{}, nil
}

func checkMountResponse(err error) string {
	if strings.Contains(err.Error(), "exit status 32") {
		return commonError.UnresponsiveMountHelperUtility
	}
	if strings.Contains(err.Error(), "exit status 1") {
		return commonError.MetadataServiceNotEnabled
	}
	if strings.Contains(err.Error(), "exit status 132") {
		return commonError.MountHelperCertificatesMissing
	}
	if strings.Contains(err.Error(), "connect: no such file") {
		return commonError.UnresponsiveMountHelperContainerUtility
	}
	return commonError.MountingTargetFailed
}
