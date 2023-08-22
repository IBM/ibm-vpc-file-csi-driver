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
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	commonError "github.com/IBM/ibm-csi-common/pkg/messages"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"go.uber.org/zap"
	"golang.org/x/net/context"
)

// TODO: expose via deployment.yaml
const (
	//socket path
	socketPath = "/tmp/mysocket.sock"
	// url
	urlPath = "http://unix/api/mount"
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

	if fsType == defaultFsType {
		err = csiNS.Mounter.Mount(stagingTargetPath, targetPath, fsType, options)
	} else if fsType == eitFsType {
		// Create payload
		payload := fmt.Sprintf(`{"stagingTargetPath":"%s","targetPath":"%s","fsType":"%s","requestID":"%s"}`, stagingTargetPath, targetPath, fsType, requestID)

		// Create a custom dialer function for Unix socket connection
		dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		}

		// Create an HTTP client with the Unix socket transport
		client := &http.Client{
			Transport: &http.Transport{
				DialContext: dialer,
			},
		}

		//Create POST request
		req, err := http.NewRequest("POST", urlPath, strings.NewReader(payload))
		if err != nil {
			ctxLogger.Error("Failed to create EIT based request. Failed wth error.")
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			ctxLogger.Error("Failed to send EIT based request. Failed with error.")
			return nil, err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			ctxLogger.Error("Error reading response.")
			return nil, err
		}

		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Mount failed. Error: %s. ResponseCode: %v", string(body), response.StatusCode)
		}

		ctxLogger.Info("Mount passed.", zap.String("Response:", string(body)), zap.Any("StatusCode:", response.StatusCode))
	} else {
		ctxLogger.Error("Invalid fsType")
		return nil, fmt.Errorf("Received inavlid fsType")
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
		err = os.Remove(targetPath)
		if err != nil {
			ctxLogger.Warn("processMount: Remove targePath Failed", zap.String("targetPath", targetPath), zap.Error(err))
		}
		return nil, commonError.GetCSIError(ctxLogger, commonError.CreateMountTargetFailed, requestID, err, targetPath)
	}

	ctxLogger.Info("CSINodeServer-processMount successfully mounted", stagingTargetPathField, targetPathField, fsTypeField, optionsField)
	return &csi.NodePublishVolumeResponse{}, nil
}
