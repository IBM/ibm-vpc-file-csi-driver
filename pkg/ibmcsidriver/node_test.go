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
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"context"

	"github.com/IBM/ibm-csi-common/pkg/utils"
	"github.com/IBM/ibm-vpc-file-csi-driver/pkg/stunnel"
	cloudProvider "github.com/IBM/ibmcloud-volume-file-vpc/pkg/ibmcloudprovider"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	//"k8s.io/utils/exec"
	//testingexec "k8s.io/utils/exec/testing"
)

const defaultVolumeID = "csiprovidervolumeid"
const defaultTargetPath = "/mnt/test"
const defaultSourcePath = "/staging"
const defaultVolumePath = "/var/volpath"

const notBlockDevice = "/for/notblocktest"

type MockStatUtils struct {
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
				StagingTargetPath: defaultSourcePath,
				Readonly:          false,
				VolumeCapability:  stdVolCap[0],
				VolumeContext:     map[string]string{NFSServerPath: "c:/abc/xyz"},
			},
			expErrCode: codes.OK,
		},
		{
			name: "Valid request with transit encryption enabled",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          defaultVolumeID,
				TargetPath:        defaultTargetPath,
				StagingTargetPath: defaultSourcePath,
				Readonly:          false,
				VolumeCapability:  stdVolCap[0],
				VolumeContext:     map[string]string{NFSServerPath: "c:/abc/xyz", IsEITEnabled: "true"},
			},
			expErrCode: codes.OK,
		},
		{
			name: "RFS profile with EIT enabled but no stunnel manager",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          defaultVolumeID,
				TargetPath:        defaultTargetPath,
				StagingTargetPath: defaultSourcePath,
				Readonly:          false,
				VolumeCapability:  stdVolCap[0],
				VolumeContext: map[string]string{
					NFSServerPath: "10.240.0.5:/share123",
					IsEITEnabled:  "true",
					ProfileLabel:  "rfs",
				},
			},
			expErrCode: codes.Internal, // Should fail - stunnel manager required for RFS+EIT
		},
		{
			name: "Valid request with DP2 profile and EIT enabled",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          defaultVolumeID,
				TargetPath:        defaultTargetPath,
				StagingTargetPath: defaultSourcePath,
				Readonly:          false,
				VolumeCapability:  stdVolCap[0],
				VolumeContext: map[string]string{
					NFSServerPath: "10.240.0.5:/share456",
					IsEITEnabled:  "true",
					ProfileLabel:  "dp2",
				},
			},
			expErrCode: codes.OK, // Should succeed with IPSEC
		},
		{
			name: "Valid request with RFS profile but EIT disabled",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          defaultVolumeID,
				TargetPath:        defaultTargetPath,
				StagingTargetPath: defaultSourcePath,
				Readonly:          false,
				VolumeCapability:  stdVolCap[0],
				VolumeContext: map[string]string{
					NFSServerPath: "10.240.0.5:/share789",
					IsEITEnabled:  "false",
					ProfileLabel:  "rfs",
				},
			},
			expErrCode: codes.OK, // Should succeed with regular NFS mount
		},
		{
			name: "Empty volume ID",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          "",
				TargetPath:        defaultTargetPath,
				StagingTargetPath: defaultSourcePath,
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
				StagingTargetPath: defaultSourcePath,
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
				StagingTargetPath: defaultSourcePath,
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

// TestNodePublishVolume_RFSWithStunnel tests RFS profile with stunnel manager configured
func TestNodePublishVolume_RFSWithStunnel(t *testing.T) {
	// Create a temporary directory for stunnel configs
	tempDir := t.TempDir()

	// Create a mock CA file
	caFile := tempDir + "/ca-bundle.crt"
	if err := os.WriteFile(caFile, []byte("mock CA cert"), 0644); err != nil {
		t.Fatalf("Failed to create mock CA file: %v", err)
	}

	// Initialize stunnel manager
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	stunnelMgr, err := stunnel.NewSimpleManager(&stunnel.Config{
		ServicesDir: tempDir + "/services",
		BasePort:    10001,
		PortRange:   100,
		CAFile:      caFile,
		LogDir:      tempDir + "/logs",
		DebugLevel:  4,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("Failed to create stunnel manager: %v", err)
	}

	// Create directories
	if err := os.MkdirAll(tempDir+"/services", 0755); err != nil {
		t.Fatalf("Failed to create services dir: %v", err)
	}
	if err := os.MkdirAll(tempDir+"/logs", 0755); err != nil {
		t.Fatalf("Failed to create logs dir: %v", err)
	}

	// Initialize IBM CSI Driver with stunnel manager
	icDriver := initIBMCSIDriver(t)
	icDriver.ns.StunnelMgr = stunnelMgr

	testCases := []struct {
		name       string
		req        *csi.NodePublishVolumeRequest
		expErrCode codes.Code
	}{
		{
			name: "RFS profile with EIT enabled and stunnel manager configured",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          "test-volume-rfs-001",
				TargetPath:        defaultTargetPath,
				StagingTargetPath: defaultSourcePath,
				Readonly:          false,
				VolumeCapability:  stdVolCap[0],
				VolumeContext: map[string]string{
					NFSServerPath:    "10.240.0.5:/share123",
					IsEITEnabled:     "true",
					ProfileLabel:     "rfs",
					FileShareIDLabel: "share-rfs-001",
				},
			},
			expErrCode: codes.OK, // Should succeed with stunnel manager
		},
		{
			name: "RFS profile with different volume",
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          "test-volume-rfs-002",
				TargetPath:        "/mnt/test2",
				StagingTargetPath: defaultSourcePath,
				Readonly:          false,
				VolumeCapability:  stdVolCap[0],
				VolumeContext: map[string]string{
					NFSServerPath:    "10.240.0.6:/share456",
					IsEITEnabled:     "true",
					ProfileLabel:     "rfs",
					FileShareIDLabel: "share-rfs-002",
				},
			},
			expErrCode: codes.OK, // Should succeed and allocate different port
		},
	}

	for _, tc := range testCases {
		t.Logf("Test case: %s", tc.name)
		resp, err := icDriver.ns.NodePublishVolume(context.Background(), tc.req)
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

		// Verify tunnel was created
		fileShareID := tc.req.VolumeContext[FileShareIDLabel]
		port, exists := stunnelMgr.GetTunnelPort(fileShareID)
		if !exists {
			t.Fatalf("Expected tunnel to be created for %s, but it doesn't exist", fileShareID)
		}
		t.Logf("Tunnel created successfully for %s on port %d", fileShareID, port)
		assert.NotNil(t, resp)
	}

	// Verify different ports were allocated
	port1, exists1 := stunnelMgr.GetTunnelPort("share-rfs-001")
	port2, exists2 := stunnelMgr.GetTunnelPort("share-rfs-002")
	if !exists1 || !exists2 {
		t.Fatalf("Expected both tunnels to exist")
	}
	if port1 == port2 {
		t.Fatalf("Expected different ports for different volumes, got same port: %d", port1)
	}
	t.Logf("Successfully allocated different ports: %d and %d", port1, port2)
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
				NodeId: "testworker",
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
		if !proto.Equal(resp, tc.resp) {
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
			name:       "Unsupported operation",
			req:        &csi.NodeExpandVolumeRequest{},
			expErrCode: codes.Unimplemented,
		},
	}
	icDriver := initIBMCSIDriver(t)
	for _, tc := range testCases {
		t.Logf("Test case: %s", tc.name)
		_, err := icDriver.ns.NodeExpandVolume(context.Background(), tc.req)
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

// This can be used in case fake cmd commands need to be called.
// func makeFakeCmd(fakeCmd *testingexec.FakeCmd, cmd string, args ...string) testingexec.FakeCommandAction {
// 	c := cmd
// 	a := args
// 	return func(cmd string, args ...string) exec.Cmd {
// 		command := testingexec.InitFakeCmd(fakeCmd, c, a...)
// 		return command
// 	}
// }
