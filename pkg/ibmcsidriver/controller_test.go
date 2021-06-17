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
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/IBM/ibm-csi-common/pkg/utils"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	providerError "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"

	cloudProvider "github.com/IBM/ibm-csi-common/pkg/ibmcloudprovider"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider/fake"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// Define "normal" parameters
	stdVolCap = []*csi.VolumeCapability{
		{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{FsType: "nfs"},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		},
	}
	stdVolCapNotSupported = []*csi.VolumeCapability{
		{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{FsType: "nfs"},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
			},
		},
	}
	stdCapRange = &csi.CapacityRange{
		RequiredBytes: 20 * 1024 * 1024 * 1024,
	}
	stdParams = map[string]string{
		Profile: "tier-10iops",
		Zone:    "myzone",
		Region:  "myregion",
	}
	stdTopology = []*csi.Topology{
		{
			Segments: map[string]string{utils.NodeZoneLabel: "myzone", utils.NodeRegionLabel: "myregion"},
		},
	}
)

func TestCreateSnapshot(t *testing.T) {
	// test cases
	testCases := []struct {
		name        string
		req         *csi.CreateSnapshotRequest
		expResponse *csi.CreateSnapshotResponse
		expErrCode  codes.Code
	}{
		{
			name:        "Unsupported operation create snapshot",
			req:         &csi.CreateSnapshotRequest{},
			expResponse: nil,
			expErrCode:  codes.Unimplemented,
		},
	}

	// Creating test logger
	_, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Run test cases
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		// Setup new driver each time so no interference
		icDriver := initIBMCSIDriver(t)

		_, err := icDriver.cs.CreateSnapshot(context.Background(), tc.req)
		if tc.expErrCode != codes.OK {
			t.Logf("Error code")
			assert.NotNil(t, err)
		}
	}
}

func TestDeleteSnapshot(t *testing.T) {
	// test cases
	testCases := []struct {
		name        string
		req         *csi.DeleteSnapshotRequest
		expResponse *csi.DeleteSnapshotResponse
		expErrCode  codes.Code
	}{
		{
			name:        "Unsupported operation delete snapshot",
			req:         &csi.DeleteSnapshotRequest{},
			expResponse: nil,
			expErrCode:  codes.OK,
		},
	}

	// Creating test logger
	_, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Run test cases
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		// Setup new driver each time so no interference
		icDriver := initIBMCSIDriver(t)

		_, err := icDriver.cs.DeleteSnapshot(context.Background(), tc.req)
		if tc.expErrCode != codes.OK {
			t.Logf("Error code")
			assert.NotNil(t, err)
		}
	}
}

func TestListSnapshots(t *testing.T) {
	// test cases
	testCases := []struct {
		name        string
		req         *csi.ListSnapshotsRequest
		expResponse *csi.ListSnapshotsResponse
		expErrCode  codes.Code
	}{
		{
			name:        "Unsupported Operation list snapshots",
			req:         &csi.ListSnapshotsRequest{},
			expResponse: nil,
			expErrCode:  codes.Unimplemented,
		},
	}

	// Creating test logger
	_, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Run test cases
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		// Setup new driver each time so no interference
		icDriver := initIBMCSIDriver(t)

		// Call CSI CreateVolume
		_, err := icDriver.cs.ListSnapshots(context.Background(), tc.req)
		if tc.expErrCode != codes.OK {
			t.Logf("Error code")
			assert.NotNil(t, err)
		}
	}
}
func TestCreateVolumeArguments(t *testing.T) {
	cap := 20
	volName := "test-name"
	iopsStr := ""
	// test cases
	testCases := []struct {
		name                          string
		req                           *csi.CreateVolumeRequest
		expVol                        *csi.Volume
		expErrCode                    codes.Code
		libVolumeResponse             *provider.Volume
		libVolumeAccessPointResp      *provider.VolumeAccessPointResponse
		libVolumeError                error
		libVolumeAccessPointError     error
		libVolumeAccessPointWaitError error
	}{
		{
			name: "Success default",
			req: &csi.CreateVolumeRequest{
				Name:               volName,
				CapacityRange:      stdCapRange,
				VolumeCapabilities: stdVolCap,
				Parameters:         stdParams,
			},
			expVol: &csi.Volume{
				CapacityBytes:      20 * 1024 * 1024 * 1024, // In byte
				VolumeId:           "testVolumeId:testVolumeAccessPointId",
				VolumeContext:      map[string]string{utils.NodeRegionLabel: "myregion", utils.NodeZoneLabel: "myzone", VolumeIDLabel: "testVolumeId:testVolumeAccessPointId", NFSServerPath: "abc:/xyz/pqr", Tag: "", VolumeCRNLabel: "", ClusterIDLabel: ""},
				AccessibleTopology: stdTopology,
			},

			libVolumeAccessPointResp: &provider.VolumeAccessPointResponse{
				VolumeID:      "testVolumeId",
				AccessPointID: "testVolumeAccessPointId",
				Status:        "Stable",
				MountPath:     "abc:/xyz/pqr",
				CreatedAt:     &time.Time{},
			},

			libVolumeResponse:             &provider.Volume{Capacity: &cap, Name: &volName, VolumeID: "testVolumeId", Iops: &iopsStr, Az: "myzone", Region: "myregion"},
			expErrCode:                    codes.OK,
			libVolumeError:                nil,
			libVolumeAccessPointError:     nil,
			libVolumeAccessPointWaitError: nil,
		},
		{
			name: "CreateVolume Access Point failure",
			req: &csi.CreateVolumeRequest{
				Name:               volName,
				CapacityRange:      stdCapRange,
				VolumeCapabilities: stdVolCap,
				Parameters:         stdParams,
			},
			expVol:                        nil,
			libVolumeAccessPointResp:      nil,
			libVolumeResponse:             &provider.Volume{Capacity: &cap, Name: &volName, VolumeID: "testVolumeId", Iops: &iopsStr, Az: "myzone", Region: "myregion"},
			expErrCode:                    codes.Internal,
			libVolumeError:                nil,
			libVolumeAccessPointWaitError: nil,
			libVolumeAccessPointError:     providerError.Message{Code: "FailedToPlaceOrder", Description: "Volume Access Point failed", Type: providerError.ProvisioningFailed},
		},
		{
			name: "Wait for CreateVolume Access Point failure",
			req: &csi.CreateVolumeRequest{
				Name:               volName,
				CapacityRange:      stdCapRange,
				VolumeCapabilities: stdVolCap,
				Parameters:         stdParams,
			},
			expVol: nil,

			libVolumeAccessPointResp: &provider.VolumeAccessPointResponse{
				VolumeID:      "testVolumeId",
				AccessPointID: "testVolumeAccessPointId",
				Status:        "Pending",
				MountPath:     "abc:/xyz/pqr",
				CreatedAt:     &time.Time{},
			},
			libVolumeResponse:             &provider.Volume{Capacity: &cap, Name: &volName, VolumeID: "testVolumeId", Iops: &iopsStr, Az: "myzone", Region: "myregion"},
			expErrCode:                    codes.Internal,
			libVolumeError:                nil,
			libVolumeAccessPointError:     nil,
			libVolumeAccessPointWaitError: providerError.Message{Code: "FailedToPlaceOrder", Description: "Volume Access Point not in stable failed", Type: providerError.ProvisioningFailed},
		},
		{
			name: "Empty volume name",
			req: &csi.CreateVolumeRequest{
				Name:               "",
				CapacityRange:      stdCapRange,
				VolumeCapabilities: stdVolCap,
				Parameters:         stdParams,
			},
			expVol:                        nil,
			libVolumeResponse:             nil,
			expErrCode:                    codes.InvalidArgument,
			libVolumeError:                nil,
			libVolumeAccessPointWaitError: nil,
			libVolumeAccessPointError:     nil,
		},
		{
			name: "Empty volume capabilities",
			req: &csi.CreateVolumeRequest{
				Name:               volName,
				CapacityRange:      stdCapRange,
				VolumeCapabilities: nil,
				Parameters:         stdParams,
			},
			expVol:                        nil,
			libVolumeResponse:             nil,
			expErrCode:                    codes.InvalidArgument,
			libVolumeError:                nil,
			libVolumeAccessPointWaitError: nil,
			libVolumeAccessPointError:     nil,
		},
		{
			name: "Not supported volume Capabilities",
			req: &csi.CreateVolumeRequest{
				Name:               volName,
				CapacityRange:      stdCapRange,
				VolumeCapabilities: stdVolCapNotSupported,
				Parameters:         stdParams,
			},
			expVol:                        nil,
			libVolumeResponse:             nil,
			expErrCode:                    codes.InvalidArgument,
			libVolumeError:                nil,
			libVolumeAccessPointWaitError: nil,
			libVolumeAccessPointError:     nil,
		},
		{
			name: "ProvisioningFailed lib error form create volume",
			req: &csi.CreateVolumeRequest{
				Name:               volName,
				CapacityRange:      stdCapRange,
				VolumeCapabilities: stdVolCap,
				Parameters:         stdParams,
			},
			expErrCode:                    codes.Internal,
			expVol:                        nil,
			libVolumeResponse:             nil,
			libVolumeAccessPointError:     nil,
			libVolumeAccessPointWaitError: nil,
			libVolumeError:                providerError.Message{Code: "FailedToPlaceOrder", Description: "Volume creation failed", Type: providerError.ProvisioningFailed},
		},
		{
			name: "InvalidRequest lib error form create volume",
			req: &csi.CreateVolumeRequest{
				Name:               volName,
				CapacityRange:      stdCapRange,
				VolumeCapabilities: stdVolCap,
				Parameters:         stdParams,
			},
			expErrCode:                    codes.Internal,
			expVol:                        nil,
			libVolumeResponse:             nil,
			libVolumeAccessPointError:     nil,
			libVolumeAccessPointWaitError: nil,
			libVolumeError:                providerError.Message{Code: "FailedToPlaceOrder", Description: "Volume creation failed", Type: providerError.InvalidRequest},
		},
		{
			name: "Other error lib error form create volume",
			req: &csi.CreateVolumeRequest{
				Name:               volName,
				CapacityRange:      stdCapRange,
				VolumeCapabilities: stdVolCap,
				Parameters:         stdParams,
			},
			expErrCode:                codes.Internal,
			expVol:                    nil,
			libVolumeResponse:         nil,
			libVolumeAccessPointError: nil,
			libVolumeError:            providerError.Message{Code: "FailedToPlaceOrder", Description: "Volume creation failed", Type: providerError.Unauthenticated},
		},
	}

	// Creating test logger
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Run test cases
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		// Setup new driver each time so no interference
		icDriver := initIBMCSIDriver(t)

		// Set the response for CreateVolume
		fakeSession, err := icDriver.cs.CSIProvider.GetProviderSession(context.Background(), logger)
		assert.Nil(t, err)
		fakeStructSession, ok := fakeSession.(*fake.FakeSession)
		assert.Equal(t, true, ok)
		fakeStructSession.CreateVolumeReturns(tc.libVolumeResponse, tc.libVolumeError)
		fakeStructSession.GetVolumeByNameReturns(tc.libVolumeResponse, tc.libVolumeError)
		fakeStructSession.GetVolumeReturns(tc.libVolumeResponse, tc.libVolumeError)
		fakeStructSession.CreateVolumeAccessPointReturns(tc.libVolumeAccessPointResp, tc.libVolumeAccessPointError)
		fakeStructSession.WaitForCreateVolumeAccessPointReturns(tc.libVolumeAccessPointResp, tc.libVolumeAccessPointWaitError)

		// Call CSI CreateVolume
		resp, err := icDriver.cs.CreateVolume(context.Background(), tc.req)
		if err != nil {
			//errorType := providerError.GetErrorType(err)
			serverError, ok := status.FromError(err)
			if !ok {
				t.Fatalf("Could not get error status code from err: %v", serverError)
			}
			if serverError.Code() != tc.expErrCode {
				t.Fatalf("Expected error code-> %v, Actual error code: %v. err : %v", tc.expErrCode, serverError.Code(), err)
			}
			continue
		}
		if tc.expErrCode != codes.OK {
			t.Fatalf("Expected error-> %v, actual no error", tc.expErrCode)
		}

		// Make sure responses match
		vol := resp.GetVolume()
		if vol == nil {
			t.Fatalf("Expected volume-> %v, Actual volume is nil", tc.expVol)
		}

		// Validate output
		if !reflect.DeepEqual(vol, tc.expVol) {
			errStr := fmt.Sprintf("Expected volume-> %#v\nTopology %#v\n\n Actual volume: %#v\nTopology %#v\n\n",
				tc.expVol, tc.expVol.GetAccessibleTopology()[0], vol, vol.GetAccessibleTopology()[0])
			for i := 0; i < len(vol.GetAccessibleTopology()); i++ {
				errStr = errStr + fmt.Sprintf("Actual topology-> %#v\nExpected toplogy-> %#v\n\n", vol.GetAccessibleTopology()[i], tc.expVol.GetAccessibleTopology()[i])
			}
			t.Errorf(errStr)
		}
	}
}

func TestDeleteVolume(t *testing.T) {
	// test cases
	testCases := []struct {
		name                               string
		req                                *csi.DeleteVolumeRequest
		expResponse                        *csi.DeleteVolumeResponse
		expErrCode                         codes.Code
		libVolumeRespError                 error
		expectedDeleteVAPErrorResponse     error
		expectedWaitDeleteVAPErrorResponse error
		libVolumeResponse                  *provider.Volume
		libVolumeAccessPointResp           *provider.VolumeAccessPointResponse
		response                           *http.Response
	}{
		{
			name:              "Success volume delete",
			req:               &csi.DeleteVolumeRequest{VolumeId: "testVolumeId:testVolumeAccessPointId"},
			expResponse:       &csi.DeleteVolumeResponse{},
			expErrCode:        codes.OK,
			libVolumeResponse: &provider.Volume{VolumeID: "testVolumeId", Az: "myzone", Region: "myregion"},
			libVolumeAccessPointResp: &provider.VolumeAccessPointResponse{
				VolumeID:      "testVolumeId",
				AccessPointID: "testVolumeAccessPointId",
				Status:        "Stable",
				MountPath:     "abc:/xyz/pqr",
				CreatedAt:     &time.Time{},
			},
			response: &http.Response{
				StatusCode: http.StatusOK,
			},
		},
		{
			name:              "Failure to delete volume access Point delete",
			req:               &csi.DeleteVolumeRequest{VolumeId: "testVolumeId:testVolumeAccessPointId"},
			expResponse:       nil,
			expErrCode:        codes.OK,
			libVolumeResponse: &provider.Volume{VolumeID: "testVolumeId", Az: "myzone", Region: "myregion"},
			libVolumeAccessPointResp: &provider.VolumeAccessPointResponse{
				VolumeID:      "testVolumeId",
				AccessPointID: "testVolumeAccessPointId",
				Status:        "Stable",
				MountPath:     "abc:/xyz/pqr",
				CreatedAt:     &time.Time{},
			},
			response:                       nil,
			expectedDeleteVAPErrorResponse: providerError.Message{Code: "DeleteVolumeAccessPointFailed", Description: "Volume access Point deletion failed", Type: providerError.DeleteVolumeAccessPointFailed},
		},
		{
			name:              "Failure to delete volume access Point due to stuck in deleting state",
			req:               &csi.DeleteVolumeRequest{VolumeId: "testVolumeId:testVolumeAccessPointId"},
			expResponse:       nil,
			expErrCode:        codes.OK,
			libVolumeResponse: &provider.Volume{VolumeID: "testVolumeId", Az: "myzone", Region: "myregion"},
			libVolumeAccessPointResp: &provider.VolumeAccessPointResponse{
				VolumeID:      "testVolumeId",
				AccessPointID: "testVolumeAccessPointId",
				Status:        "Deleting",
				MountPath:     "abc:/xyz/pqr",
				CreatedAt:     &time.Time{},
			},
			response:                           nil,
			expectedDeleteVAPErrorResponse:     nil,
			expectedWaitDeleteVAPErrorResponse: providerError.Message{Code: "DeleteVolumeAccessPointFailed", Description: "Volume access Point deletion failed", Type: providerError.DeleteVolumeAccessPointFailed},
		},
		{
			name:        "Failed volume delete with multiple access point exists for volume",
			req:         &csi.DeleteVolumeRequest{VolumeId: "testVolumeId:testVolumeAccessPointId"},
			expResponse: nil,
			expErrCode:  codes.Internal,
			libVolumeResponse: &provider.Volume{VolumeID: "testVolumeId", Az: "myzone", Region: "myregion", VPCVolume: provider.VPCVolume{
				Href:                "",
				ResourceGroup:       &provider.ResourceGroup{},
				VolumeEncryptionKey: &provider.VolumeEncryptionKey{},
				Profile:             &provider.Profile{},
				CRN:                 "",
				VPCBlockVolume:      provider.VPCBlockVolume{},
				VPCFileVolume: provider.VPCFileVolume{
					VolumeAccessPoints: &[]provider.VolumeAccessPoint{
						{
							ID: "testVolumeAccessPointId",
							VPC: &provider.VPC{
								ID: "1234",
							},
						},
						{
							ID: "testVolumeAccessPointId",
							VPC: &provider.VPC{
								ID: "1234",
							},
						},
					},
				},
			}},
		},
		{
			name:        "Success volume delete in case volume not found",
			req:         &csi.DeleteVolumeRequest{VolumeId: "testVolumeId:testVolumeAccessPointId"},
			expResponse: &csi.DeleteVolumeResponse{},
			expErrCode:  codes.OK,
		},
		{
			name:        "Failed volume delete with volume id empty",
			req:         &csi.DeleteVolumeRequest{VolumeId: ""},
			expResponse: nil,
			expErrCode:  codes.InvalidArgument,
		},
		{
			name:               "Failed from lib volume delete failed",
			req:                &csi.DeleteVolumeRequest{VolumeId: "testVolumeId:testVolumeAccessPointId"},
			expResponse:        nil,
			expErrCode:         codes.Internal,
			libVolumeRespError: providerError.Message{Code: "FailedToDeleteVolume", Description: "Volume deletion failed", Type: providerError.DeletionFailed},
			libVolumeResponse:  &provider.Volume{VolumeID: "testVolumeId", Az: "myzone", Region: "myregion"},
		},
		{
			name:               "Volume ID invalid format",
			req:                &csi.DeleteVolumeRequest{VolumeId: "testVolumeId"},
			expResponse:        nil,
			expErrCode:         codes.Internal,
			libVolumeRespError: providerError.Message{Code: "FailedToDeleteVolume", Description: "Volume deletion failed", Type: providerError.DeletionFailed},
			libVolumeResponse:  nil,
		},
	}

	// Creating test logger
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Run test cases
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		// Setup new driver each time so no interference
		icDriver := initIBMCSIDriver(t)

		// Set the response for DeleteVolume
		fakeSession, err := icDriver.cs.CSIProvider.GetProviderSession(context.Background(), logger)
		assert.Nil(t, err)
		fakeStructSession, ok := fakeSession.(*fake.FakeSession)
		assert.Equal(t, true, ok)
		fakeStructSession.DeleteVolumeReturns(tc.libVolumeRespError)
		fakeStructSession.GetVolumeByNameReturns(tc.libVolumeResponse, nil)
		fakeStructSession.GetVolumeReturns(tc.libVolumeResponse, nil)
		fakeStructSession.DeleteVolumeAccessPointReturns(tc.response, tc.expectedDeleteVAPErrorResponse)
		fakeStructSession.WaitForDeleteVolumeAccessPointReturns(tc.expectedWaitDeleteVAPErrorResponse)

		// Call CSI CreateVolume
		response, err := icDriver.cs.DeleteVolume(context.Background(), tc.req)
		if tc.expErrCode != codes.OK {
			assert.NotNil(t, err)
		}
		assert.Equal(t, tc.expResponse, response)
	}
}

func TestValidateVolumeCapabilities(t *testing.T) {
	// test cases
	testCases := []struct {
		name              string
		req               *csi.ValidateVolumeCapabilitiesRequest
		expResponse       *csi.ValidateVolumeCapabilitiesResponse
		expErrCode        codes.Code
		libGetVolumeError error
	}{
		{
			name: "Success validate volume capabilities",
			req: &csi.ValidateVolumeCapabilitiesRequest{VolumeId: "volumeid",
				VolumeCapabilities: []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER}}},
			},
			expResponse: &csi.ValidateVolumeCapabilitiesResponse{
				Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
					VolumeCapabilities: []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER}}},
				},
			},
			expErrCode:        codes.OK,
			libGetVolumeError: nil,
		},
		{
			name: "Passing nil volume capabilities",
			req: &csi.ValidateVolumeCapabilitiesRequest{VolumeId: "volumeid",
				VolumeCapabilities: nil,
			},
			expResponse:       nil,
			expErrCode:        codes.InvalidArgument,
			libGetVolumeError: nil,
		},
		{
			name: "Passing nil volume ID",
			req: &csi.ValidateVolumeCapabilitiesRequest{VolumeId: "",
				VolumeCapabilities: []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER}}},
			},
			expResponse:       nil,
			expErrCode:        codes.InvalidArgument,
			libGetVolumeError: nil,
		},
		{
			name: "Get volume failed",
			req: &csi.ValidateVolumeCapabilitiesRequest{VolumeId: "volume-not-found-ID",
				VolumeCapabilities: []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER}}},
			},
			expResponse: nil,
			expErrCode:  codes.NotFound,
			libGetVolumeError: providerError.Message{
				Code:        "StorageFindFailedWithVolumeName",
				Description: "Volume not found by volume ID",
				Type:        providerError.RetrivalFailed,
			},
		},
		{
			name: "Internal error while getting volume details",
			req: &csi.ValidateVolumeCapabilitiesRequest{VolumeId: "volumeid",
				VolumeCapabilities: []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER}}},
			},
			expResponse: nil,
			expErrCode:  codes.Internal,
			libGetVolumeError: providerError.Message{
				Code:        "StorageFindFailed",
				Description: "Internal error",
				Type:        providerError.PermissionDenied, // any error apartfrom providerError.RetrivalFailed
			},
		},
	}

	// Creating test logger
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Run test cases
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		// Setup new driver each time so no interference
		icDriver := initIBMCSIDriver(t)

		// Set the response for GetVolume
		fakeSession, err := icDriver.cs.CSIProvider.GetProviderSession(context.Background(), logger)
		assert.Nil(t, err)
		fakeStructSession, ok := fakeSession.(*fake.FakeSession)
		assert.Equal(t, true, ok)
		fakeStructSession.GetVolumeReturns(nil, tc.libGetVolumeError)

		// Call CSI CreateVolume
		response, err := icDriver.cs.ValidateVolumeCapabilities(context.Background(), tc.req)
		if tc.expErrCode != codes.OK {
			t.Logf("Error code")
			assert.NotNil(t, err)
		}
		// This is because csi.ControllerPublishVolumeResponse contains request ID which is always different
		// hence better to compair all fields
		assert.Equal(t, tc.expResponse, response)
	}
}

func TestListVolumes(t *testing.T) {
	limit := 100
	testCases := []struct {
		name            string
		maxEntries      int32
		expectedEntries int
		expectedErr     bool
		expErrCode      codes.Code
		libVolumeError  error
	}{
		{
			name:            "normal",
			expectedEntries: 50,
			expectedErr:     false,
			expErrCode:      codes.OK,
			libVolumeError:  nil,
		},
		{
			name:            "fine amount of entries",
			maxEntries:      40,
			expectedEntries: 40,
			expectedErr:     false,
			expErrCode:      codes.OK,
			libVolumeError:  nil,
		},
		{
			name:            "too many entries, but defaults to 100",
			maxEntries:      101,
			expectedEntries: 100,
			expectedErr:     false,
			expErrCode:      codes.OK,
			libVolumeError:  nil,
		},
		{
			name:           "negative entries",
			maxEntries:     -1,
			expectedErr:    true,
			expErrCode:     codes.InvalidArgument,
			libVolumeError: providerError.Message{Code: "InvalidListVolumesLimit", Description: "The value '-1' specified in the limit parameter of the list volume call is not valid.", Type: providerError.InvalidRequest},
		},
		{
			name:           "Invalid start volume ID",
			maxEntries:     10,
			expectedErr:    true,
			expErrCode:     codes.Aborted,
			libVolumeError: providerError.Message{Code: "StartVolumeIDNotFound", Description: "The volume ID specified in the start parameter of the list volume call could not be found.", Type: providerError.InvalidRequest},
		},
		{
			name:           "internal error",
			maxEntries:     10,
			expectedErr:    true,
			expErrCode:     codes.Internal,
			libVolumeError: providerError.Message{Code: "ListVolumesFailed", Description: "Unable to fetch list of volumes.", Type: providerError.RetrivalFailed},
		},
	}

	// Creating test logger
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		// Setup new driver each time so no interference
		icDriver := initIBMCSIDriver(t)

		// Set the response for CreateVolume
		fakeSession, err := icDriver.cs.CSIProvider.GetProviderSession(context.Background(), logger)
		assert.Nil(t, err)
		fakeStructSession, ok := fakeSession.(*fake.FakeSession)
		assert.Equal(t, true, ok)

		maxEntries := int(tc.maxEntries)
		if maxEntries == 0 {
			maxEntries = 50
		} else if maxEntries > limit {
			maxEntries = limit
		}

		volList := &provider.VolumeList{}
		if !tc.expectedErr {
			volList = createVolume(maxEntries)
		}
		fakeStructSession.ListVolumesReturns(volList, tc.libVolumeError)

		lvr := &csi.ListVolumesRequest{
			MaxEntries: tc.maxEntries,
		}
		resp, err := icDriver.cs.ListVolumes(context.TODO(), lvr)
		if tc.expErrCode != codes.OK {
			assert.NotNil(t, err)
		}
		if tc.expectedErr && err == nil {
			t.Fatalf("Got no error when expecting an error")
		}
		if err != nil {
			if !tc.expectedErr {
				t.Fatalf("Got error '%v', expecting none", err)
			}
		} else {
			if len(resp.Entries) != tc.expectedEntries {
				t.Fatalf("Got '%v' entries, expected '%v'", len(resp.Entries), tc.expectedEntries)
			}
			if resp.NextToken != volList.Next {
				t.Fatalf("Got '%v' next_token, expected '%v'", resp.NextToken, volList.Next)
			}
		}
	}
}

func TestGetCapacity(t *testing.T) {
	// test cases
	testCases := []struct {
		name        string
		req         *csi.GetCapacityRequest
		expResponse *csi.GetCapacityResponse
		expErrCode  codes.Code
	}{
		{
			name:        "Success get capacity",
			req:         &csi.GetCapacityRequest{},
			expResponse: nil,
			expErrCode:  codes.OK,
		},
	}

	// Creating test logger
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Run test cases
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		// Setup new driver each time so no interference
		icDriver := initIBMCSIDriver(t)

		fakeSession, err := icDriver.cs.CSIProvider.GetProviderSession(context.Background(), logger)
		assert.Nil(t, err)
		/*fakeStructSession*/ _, ok := fakeSession.(*fake.FakeSession)
		assert.Equal(t, true, ok)

		// Call CSI CreateVolume
		response, err := icDriver.cs.GetCapacity(context.Background(), tc.req)
		if tc.expErrCode != codes.OK {
			t.Logf("Error code")
			assert.NotNil(t, err)
		}
		assert.Equal(t, tc.expResponse, response)
	}
}

func TestControllerPublishVolume(t *testing.T) {
	// test cases
	testCases := []struct {
		name        string
		req         *csi.ControllerPublishVolumeRequest
		expResponse *csi.ControllerPublishVolumeResponse
		expErrCode  codes.Code
	}{
		{
			name:        "Failed unsupported Controller Publish Volume",
			req:         &csi.ControllerPublishVolumeRequest{VolumeId: "vol123", NodeId: "node123", VolumeCapability: &csi.VolumeCapability{AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER}}},
			expResponse: nil,
			expErrCode:  codes.Unimplemented,
		},
	}

	// Creating test logger
	_, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Run test cases
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		// Setup new driver each time so no interference
		icDriver := initIBMCSIDriver(t)

		// Call CSI CreateVolume
		_, err := icDriver.cs.ControllerPublishVolume(context.Background(), tc.req)
		if tc.expErrCode != codes.OK {
			assert.NotNil(t, err)
		}
	}
}

func TestControllerUnpublishVolume(t *testing.T) {
	// test cases
	testCases := []struct {
		name        string
		req         *csi.ControllerUnpublishVolumeRequest
		expResponse *csi.ControllerUnpublishVolumeResponse
		expErrCode  codes.Code
	}{
		{
			name:        "Failed unsupported Controller UnPublish Volume",
			req:         &csi.ControllerUnpublishVolumeRequest{VolumeId: "volumeid", NodeId: "nodeid"},
			expResponse: nil,
			expErrCode:  codes.Unimplemented,
		},
	}

	// Creating test logger
	_, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Run test cases
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		// Setup new driver each time so no interference
		icDriver := initIBMCSIDriver(t)

		// Call CSI CreateVolume
		_, err := icDriver.cs.ControllerUnpublishVolume(context.Background(), tc.req)
		if tc.expErrCode != codes.OK {
			assert.NotNil(t, err)
		}
	}
}

func TestControllerGetCapabilities(t *testing.T) {
	// test cases
	testCases := []struct {
		name        string
		req         *csi.ControllerGetCapabilitiesRequest
		expResponse *csi.ControllerGetCapabilitiesResponse
		expErrCode  codes.Code
	}{
		{
			name: "Success controller get capabilities",
			req:  &csi.ControllerGetCapabilitiesRequest{},
			expResponse: &csi.ControllerGetCapabilitiesResponse{
				Capabilities: []*csi.ControllerServiceCapability{
					{Type: &csi.ControllerServiceCapability_Rpc{Rpc: &csi.ControllerServiceCapability_RPC{Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME}}},
					//{Type: &csi.ControllerServiceCapability_Rpc{Rpc: &csi.ControllerServiceCapability_RPC{Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME}}},
					{Type: &csi.ControllerServiceCapability_Rpc{Rpc: &csi.ControllerServiceCapability_RPC{Type: csi.ControllerServiceCapability_RPC_LIST_VOLUMES}}},
					//{Type: &csi.ControllerServiceCapability_Rpc{Rpc: &csi.ControllerServiceCapability_RPC{Type: csi.ControllerServiceCapability_RPC_EXPAND_VOLUME}}},
					// &csi.ControllerServiceCapability{Type: &csi.ControllerServiceCapability_Rpc{Rpc: &csi.ControllerServiceCapability_RPC{Type: csi.ControllerServiceCapability_RPC_GET_CAPACITY}}},
					// &csi.ControllerServiceCapability{Type: &csi.ControllerServiceCapability_Rpc{Rpc: &csi.ControllerServiceCapability_RPC{Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT}}},
					// &csi.ControllerServiceCapability{Type: &csi.ControllerServiceCapability_Rpc{Rpc: &csi.ControllerServiceCapability_RPC{Type: csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS}}},
					// &csi.ControllerServiceCapability{Type: &csi.ControllerServiceCapability_Rpc{Rpc: &csi.ControllerServiceCapability_RPC{Type: csi.ControllerServiceCapability_RPC_PUBLISH_READONLY}}},
				},
			},
			expErrCode: codes.OK,
		},
	}

	// Creating test logger
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Run test cases
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		// Setup new driver each time so no interference
		icDriver := initIBMCSIDriver(t)

		fakeSession, err := icDriver.cs.CSIProvider.GetProviderSession(context.Background(), logger)
		assert.Nil(t, err)
		/*fakeStructSession*/ _, ok := fakeSession.(*fake.FakeSession)
		assert.Equal(t, true, ok)

		// Call CSI CreateVolume
		response, err := icDriver.cs.ControllerGetCapabilities(context.Background(), tc.req)
		if tc.expErrCode != codes.OK {
			t.Logf("Error code")
			assert.NotNil(t, err)
		}

		if !reflect.DeepEqual(response, tc.expResponse) {
			assert.Equal(t, tc.expResponse, response)
		}
	}
}
func TestControllerExpandVolume(t *testing.T) {
	// test cases
	testCases := []struct {
		name        string
		req         *csi.ControllerExpandVolumeRequest
		expResponse *csi.ControllerExpandVolumeResponse
		expErrCode  codes.Code
	}{
		{
			name:        "Expand volume unsupportedfailed",
			req:         &csi.ControllerExpandVolumeRequest{VolumeId: "volumeid", CapacityRange: stdCapRange},
			expResponse: nil,
			expErrCode:  codes.Unimplemented,
		},
	}

	// Creating test logger
	_, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Run test cases
	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		// Setup new driver each time so no interference
		icDriver := initIBMCSIDriver(t)

		// Call CSI CreateVolume
		_, err := icDriver.cs.ControllerExpandVolume(context.Background(), tc.req)
		if tc.expErrCode != codes.OK {
			t.Logf("Error code")
			assert.NotNil(t, err)
		}
	}
}

func createVolume(maxEntries int) *provider.VolumeList {
	volList := &provider.VolumeList{}
	cap := 10
	for i := 0; i <= maxEntries; i++ {
		volName := "unit-test-volume" + strconv.Itoa(i)
		vol := &provider.Volume{
			VolumeID: fmt.Sprintf("vol-uuid-test-vol-%s", uuid.New().String()[:10]),
			Name:     &volName,
			Region:   "my-region",
			Capacity: &cap,
		}
		if i == maxEntries {
			volList.Next = vol.VolumeID
		} else {
			volList.Volumes = append(volList.Volumes, vol)
		}
	}
	return volList
}
