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
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/IBM/ibm-csi-common/pkg/mountmanager"
	"github.com/IBM/ibm-csi-common/pkg/utils"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultVolumeID = "csiprovidervolumeid"
const defaultTargetPath = "/mnt/test"
const defaultStagingPath = "/staging"
const defaultVolumePath = "/var/volpath"

const notBlockDevice = "/for/notblocktest"

type MockStatUtils struct {
}

type MockMountUtils struct {
}

// Resize expands the fs
func (mu *MockMountUtils) Resize(mounter mountmanager.Mounter, devicePath string, deviceMountPath string) (bool, error) {
	if strings.Contains(deviceMountPath, "fake-") {
		return false, fmt.Errorf("failed to resize fs")
	} else if strings.Contains(deviceMountPath, "valid-") {
		return true, nil
	}
	return false, fmt.Errorf("failed to resize fs")
}

func (su *MockStatUtils) FSInfo(path string) (int64, int64, int64, int64, int64, int64, error) {
	return 1, 1, 1, 1, 1, 1, nil
}

func (su *MockStatUtils) DeviceInfo(path string) (int64, error) {
	if strings.Contains(path, "errordevicepath") {
		return 1, errors.New("error in getting device info")
	}
	return 1, nil
}

func (su *MockStatUtils) IsBlockDevice(devicePath string) (bool, error) {
	if strings.Contains(devicePath, "errorblock") {
		return false, errors.New("error in IsBlockDevice check")
	} else if strings.Contains(devicePath, "notblock") {
		return false, nil
	}
	return true, nil
}

func (su *MockStatUtils) IsDevicePathNotExist(devicePath string) bool {
	return strings.Contains(devicePath, "correctdevicepath")
}

func TestNodePublishVolume(t *testing.T) {
	testCases := []struct {
		name       string
		req        *csi.NodePublishVolumeRequest
		expErrCode codes.Code
	}{
		{
			name: "Valid request",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          defaultVolumeID,
				TargetPath:        defaultTargetPath,
				StagingTargetPath: defaultStagingPath,
				Readonly:          false,
				VolumeCapability:  stdVolCap[0],
				VolumeContext:     map[string]string{NFSServerPath: "c:/abc/xyz"},
			},
			expErrCode: codes.OK,
		},
		{
			name: "Empty volume ID",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          "",
				TargetPath:        defaultTargetPath,
				StagingTargetPath: defaultStagingPath,
				Readonly:          false,
				VolumeCapability:  stdVolCap[0],
			},
			expErrCode: codes.InvalidArgument,
		},
		{
			name: "Empty staging target path",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          "testvolumeid",
				TargetPath:        defaultTargetPath,
				StagingTargetPath: "",
				Readonly:          false,
				VolumeCapability:  stdVolCap[0],
			},
			expErrCode: codes.InvalidArgument,
		},
		{
			name: "Empty target path",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          "testvolumeid",
				TargetPath:        "",
				StagingTargetPath: defaultTargetPath,
				Readonly:          false,
				VolumeCapability:  stdVolCap[0],
			},
			expErrCode: codes.InvalidArgument,
		},
		{
			name: "Empty volume capabilities",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          "testvolumeid",
				TargetPath:        defaultTargetPath,
				StagingTargetPath: defaultStagingPath,
				Readonly:          false,
				VolumeCapability:  nil,
			},
			expErrCode: codes.InvalidArgument,
		},
		{
			name: "Not supported volume capabilities",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          "testvolumeid",
				TargetPath:        defaultTargetPath,
				StagingTargetPath: defaultStagingPath,
				Readonly:          false,
				VolumeCapability:  stdVolCapNotSupported[0],
			},
			expErrCode: codes.InvalidArgument,
		},
	}

	icDriver := initIBMCSIDriver(t)

	for _, tc := range testCases {
		t.Logf("Test case: %s", tc.name)
		_, err := icDriver.ns.NodePublishVolume(context.Background(), tc.req)
		if err != nil {
			serverError, ok := status.FromError(err)
			if !ok {
				t.Fatalf("Could not get error status code from err: %v", err)
			}
			if serverError.Code() != tc.expErrCode {
				t.Fatalf("Expected error code: %v, got: %v. err : %v", tc.expErrCode, serverError.Code(), err)
			}
			continue
		}
		if tc.expErrCode != codes.OK {
			t.Fatalf("Expected error: %v, got no error", tc.expErrCode)
		}
	}
}

func TestNodeUnpublishVolume(t *testing.T) {
	testCases := []struct {
		name       string
		req        *csi.NodeUnpublishVolumeRequest
		expErrCode codes.Code
	}{
		{
			name: "Valid request",
			req: &csi.NodeUnpublishVolumeRequest{
				VolumeId:   defaultVolumeID,
				TargetPath: defaultTargetPath,
			},
			expErrCode: codes.OK,
		},
		{
			name: "Empty volume ID",
			req: &csi.NodeUnpublishVolumeRequest{
				VolumeId:   "",
				TargetPath: defaultTargetPath,
			},
			expErrCode: codes.InvalidArgument,
		},
		{
			name: "Empty target path",
			req: &csi.NodeUnpublishVolumeRequest{
				VolumeId:   defaultVolumeID,
				TargetPath: "",
			},
			expErrCode: codes.InvalidArgument,
		},
	}

	icDriver := initIBMCSIDriver(t)

	for _, tc := range testCases {
		t.Logf("Test case: %s", tc.name)
		_, err := icDriver.ns.NodeUnpublishVolume(context.Background(), tc.req)
		if err != nil {
			serverError, ok := status.FromError(err)
			if !ok {
				t.Fatalf("Could not get error status code from err: %v", err)
			}
			if serverError.Code() != tc.expErrCode {
				t.Fatalf("Expected error code: %v, got: %v. err : %v", tc.expErrCode, serverError.Code(), err)
			}
			continue
		}
		if tc.expErrCode != codes.OK {
			t.Fatalf("Expected error: %v, got no error", tc.expErrCode)
		}
	}
}

func TestNodeGetCapabilities(t *testing.T) {
	req := &csi.NodeGetCapabilitiesRequest{}

	icDriver := initIBMCSIDriver(t)
	_, err := icDriver.ns.NodeGetCapabilities(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpedted error: %v", err)
	}
}

func TestNodeGetInfo(t *testing.T) {
	cores := runtime.NumCPU()
	var maxVolumesPerNode int64 = DefaultVolumesPerNode
	if cores >= MinimumCoresWithMaximumAttachableVolumes {
		maxVolumesPerNode = MaxVolumesPerNode
	}

	testCases := []struct {
		name          string
		req           *csi.NodeGetInfoRequest
		resetMetadata bool
		resp          *csi.NodeGetInfoResponse
		expErrCode    codes.Code
		expError      error
	}{
		{
			name:          "Success to get node info",
			req:           &csi.NodeGetInfoRequest{},
			resetMetadata: false,
			resp: &csi.NodeGetInfoResponse{
				NodeId:            "testworker",
				MaxVolumesPerNode: maxVolumesPerNode,
				AccessibleTopology: &csi.Topology{
					Segments: map[string]string{
						utils.NodeRegionLabel: "testregion",
						utils.NodeZoneLabel:   "testzone",
					},
				},
			},
			expErrCode: codes.OK,
			expError:   nil,
		},
		{
			name:          "No node data service set",
			req:           &csi.NodeGetInfoRequest{},
			resetMetadata: true,
			resp:          nil,
			expErrCode:    codes.NotFound,
			expError:      fmt.Errorf("any error is fine because error code is getting verified"),
		},
	}

	icDriver := initIBMCSIDriver(t)
	for _, tc := range testCases {
		if tc.resetMetadata {
			icDriver.ns.Metadata = nil
		}
		response, err := icDriver.ns.NodeGetInfo(context.Background(), tc.req)
		if err != nil {
			serverError, ok := status.FromError(err)
			if !ok {
				t.Fatalf("Could not get error status code from err: %v", err)
			}
			assert.Equal(t, tc.expErrCode, serverError.Code())
		} else {
			assert.Nil(t, err)
			assert.Equal(t, tc.resp, response)
		}
	}
}

func TestNodeGetVolumeStats(t *testing.T) {
	testCases := []struct {
		name       string
		req        *csi.NodeGetVolumeStatsRequest
		resp       *csi.NodeGetVolumeStatsResponse
		expErrCode codes.Code
		expError   string
	}{
		{
			name: "Empty volume ID",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId:   "",
				VolumePath: defaultVolumePath,
			},
			resp:       nil,
			expErrCode: codes.InvalidArgument,
			expError:   "",
		},
		{
			name: "Empty volume path",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId:   defaultVolumeID,
				VolumePath: "",
			},
			resp:       nil,
			expErrCode: codes.InvalidArgument,
			expError:   "",
		},
		{
			name: "Mode is File",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId:   defaultVolumeID,
				VolumePath: notBlockDevice,
			},
			resp: &csi.NodeGetVolumeStatsResponse{
				Usage: []*csi.VolumeUsage{
					{
						Available: 1,
						Total:     1,
						Used:      1,
						Unit:      1,
					},
					{
						Available: 1,
						Total:     1,
						Used:      1,
						Unit:      2,
					},
				},
			},
			expErrCode: codes.OK,
			expError:   "",
		},
	}
	icDriver := initIBMCSIDriver(t)
	for _, tc := range testCases {
		t.Logf("Test case: %s", tc.name)
		fmt.Println(tc.resp)
		resp, err := icDriver.ns.NodeGetVolumeStats(context.Background(), tc.req)
		if !reflect.DeepEqual(resp, tc.resp) {
			t.Fatalf("Expected response: %v, got: %v", tc.resp, resp)
		}
		if tc.expError != "" {
			assert.NotNil(t, err)
			continue
		}
		if err != nil {
			serverError, ok := status.FromError(err)
			if !ok {
				t.Fatalf("Could not get error status code from err: %v", err)
			}
			if serverError.Code() != tc.expErrCode {
				t.Fatalf("Expected error code: %v, got: %v. err : %v", tc.expErrCode, serverError.Code(), err)
			}
			continue
		}
		if tc.expErrCode != codes.OK {
			t.Fatalf("Expected error: %v, got no error", tc.expErrCode)
		}
	}
}

func TestNodeExpandVolume(t *testing.T) {
	testCases := []struct {
		name       string
		req        *csi.NodeExpandVolumeRequest
		res        *csi.NodeExpandVolumeResponse
		expErrCode codes.Code
	}{
		{
			name: "Empty volumePath",
			req: &csi.NodeExpandVolumeRequest{
				VolumeId:   defaultVolumeID,
				VolumePath: "",
			},
			res:        nil,
			expErrCode: codes.InvalidArgument,
		},
		{
			name: "Invalid volumePath",
			req: &csi.NodeExpandVolumeRequest{
				VolumeId:   defaultVolumeID,
				VolumePath: "/invalid-volPath",
			},
			res:        nil,
			expErrCode: codes.NotFound,
		},
		{
			name: "valid volumePath",
			req: &csi.NodeExpandVolumeRequest{
				VolumeId:   defaultVolumeID,
				VolumePath: "valid-vol-path",
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 20 * 1024 * 1024 * 1024,
				},
			},
			res:        &csi.NodeExpandVolumeResponse{CapacityBytes: stdCapRange.RequiredBytes},
			expErrCode: codes.OK,
		},
		{
			name: "volumePath not mounted",
			req: &csi.NodeExpandVolumeRequest{
				VolumeId:   defaultVolumeID,
				VolumePath: "fake-volPath",
			},
			res:        nil,
			expErrCode: codes.NotFound,
		},
	}
	icDriver := initIBMCSIDriver(t)
	_ = os.MkdirAll("valid-vol-path", os.FileMode(0755))
	_ = icDriver.ns.Mounter.Mount("valid-devicePath", "valid-vol-path", "nfs", []string{"hard", "nfsvers=4.0", "sec=sys"})
	mountmgr = &MockMountUtils{}
	for _, tc := range testCases {
		t.Logf("Test case: %s", tc.name)
		response, err := icDriver.ns.NodeExpandVolume(context.Background(), tc.req)
		if err != nil {
			serverError, ok := status.FromError(err)
			if !ok {
				t.Fatalf("Could not get error status code from err: %v", err)
			}
			if serverError.Code() != tc.expErrCode {
				t.Fatalf("Expected error code: %v, got: %v. err : %v", tc.expErrCode, serverError.Code(), err)
			}
			if response != tc.res {
				t.Fatalf("Expected response: %v, got: %v.", tc.res, response)
			}
			continue
		}
		if tc.expErrCode != codes.OK {
			t.Fatalf("Expected error: %v, got no error", tc.expErrCode)
		}
	}
	_ = os.RemoveAll("valid-vol-path")
}

func TestNodeStageVolume(t *testing.T) {
	testCases := []struct {
		name       string
		req        *csi.NodeStageVolumeRequest
		expErrCode codes.Code
	}{

		{
			name:       "Unsupported operation",
			req:        &csi.NodeStageVolumeRequest{},
			expErrCode: codes.Unimplemented,
		},
	}

	icDriver := initIBMCSIDriver(t)
	for _, tc := range testCases {
		t.Logf("Test case: %s", tc.name)
		_, err := icDriver.ns.NodeStageVolume(context.Background(), tc.req)
		if err != nil {
			serverError, ok := status.FromError(err)
			if !ok {
				t.Fatalf("Could not get error status code from err: %v", err)
			}
			if serverError.Code() != tc.expErrCode {
				t.Fatalf("Expected error code: %v, got: %v. err : %v", tc.expErrCode, serverError.Code(), err)
			}
			continue
		}
		if tc.expErrCode != codes.OK {
			t.Fatalf("Expected error: %v, got no error", tc.expErrCode)
		}
	}
}

func TestNodeUnstageVolume(t *testing.T) {
	testCases := []struct {
		name       string
		req        *csi.NodeUnstageVolumeRequest
		expErrCode codes.Code
	}{
		{
			name:       "Unsupported Operation",
			req:        &csi.NodeUnstageVolumeRequest{},
			expErrCode: codes.Unimplemented,
		},
	}

	icDriver := initIBMCSIDriver(t)
	for _, tc := range testCases {
		t.Logf("Test case: %s", tc.name)
		_, err := icDriver.ns.NodeUnstageVolume(context.Background(), tc.req)
		if err != nil {
			serverError, ok := status.FromError(err)
			if !ok {
				t.Fatalf("Could not get error status code from err: %v", err)
			}
			if serverError.Code() != tc.expErrCode {
				t.Fatalf("Expected error code: %v, got: %v. err : %v", tc.expErrCode, serverError.Code(), err)
			}
			continue
		}
		if tc.expErrCode != codes.OK {
			t.Fatalf("Expected error: %v, got no error", tc.expErrCode)
		}
	}
}

func TestIsDevicePathNotExist(t *testing.T) {
	testCases := []struct {
		name          string
		reqDevicePath string
		expResp       bool
	}{
		{
			name:          "Success device path not exists",
			reqDevicePath: "/tmp111111111111111",
			expResp:       true,
		},
		{
			name:          "Device path exists",
			reqDevicePath: "/tmp",
			expResp:       false,
		},
	}

	statUtils := &VolumeStatUtils{}
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		isBlock := statUtils.IsDevicePathNotExist(tc.reqDevicePath)
		assert.Equal(t, tc.expResp, isBlock)
	}
}
