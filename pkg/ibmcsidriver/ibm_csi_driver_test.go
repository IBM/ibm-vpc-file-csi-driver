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
	"testing"

	mountManager "github.com/IBM/ibm-csi-common/pkg/mountmanager"
	cloudProvider "github.com/IBM/ibmcloud-volume-file-vpc/pkg/ibmcloudprovider"
	nodeMetadata "github.com/IBM/ibmcloud-volume-file-vpc/pkg/metadata"
	nodeInfo "github.com/IBM/ibmcloud-volume-file-vpc/pkg/metadata/fake"
	"github.com/stretchr/testify/assert"
	testingexec "k8s.io/utils/exec/testing"
)

func initIBMCSIDriver(t *testing.T, fakeActions ...testingexec.FakeCommandAction) *IBMCSIDriver {
	vendorVersion := "test-vendor-version-1.1.2"
	driver := "mydriver"

	// Creating test logger
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()
	icDriver := GetIBMCSIDriver()
	// Create fake provider and mounter
	provider, _ := cloudProvider.NewFakeIBMCloudStorageProvider("", logger)
	var mounter mountManager.Mounter
	if len(fakeActions) != 0 {
		mounter = mountManager.NewFakeNodeMounterWithCustomActions(fakeActions)
	} else {
		mounter = mountManager.NewFakeNodeMounter()
	}
	statsUtil := &MockStatUtils{}

	fakeNodeData := nodeMetadata.FakeNodeMetadata{}
	fakeNodeInfo := nodeInfo.FakeNodeInfo{}
	fakeNodeData.GetRegionReturns("testregion")
	fakeNodeData.GetZoneReturns("testzone")
	fakeNodeData.GetWorkerIDReturns("testworker")
	fakeNodeInfo.NewNodeMetadataReturns(&fakeNodeData, nil)

	// Setup the IBM CSI driver
	err := icDriver.SetupIBMCSIDriver(provider, mounter, statsUtil, &fakeNodeData, &fakeNodeInfo, logger, driver, vendorVersion)
	if err != nil {
		t.Fatalf("Failed to setup IBM CSI Driver: %v", err)
	}

	return icDriver
}

func TestSetupIBMCSIDriver(t *testing.T) {
	// success setting up driver
	driver := initIBMCSIDriver(t)
	assert.NotNil(t, driver)

	// common code
	// Creating test logger
	vendorVersion := "test-vendor-version-1.1.2"
	name := "mydriver"
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()
	icDriver := GetIBMCSIDriver()

	// Create fake provider and mounter
	provider, _ := cloudProvider.NewFakeIBMCloudStorageProvider("", logger)
	mounter := mountManager.NewFakeNodeMounter()
	statsUtil := &MockStatUtils{}

	fakeNodeData := nodeMetadata.FakeNodeMetadata{}
	fakeNodeInfo := nodeInfo.FakeNodeInfo{}
	fakeNodeData.GetRegionReturns("testregion")
	fakeNodeData.GetZoneReturns("testzone")
	fakeNodeData.GetWorkerIDReturns("testworker")
	fakeNodeInfo.NewNodeMetadataReturns(&fakeNodeData, nil)

	// Failed setting up driver, provider nil
	err := icDriver.SetupIBMCSIDriver(nil, mounter, statsUtil, &fakeNodeData, &fakeNodeInfo, logger, name, vendorVersion)
	assert.NotNil(t, err)

	// Failed setting up driver, mounter nil
	err = icDriver.SetupIBMCSIDriver(provider, nil, statsUtil, &fakeNodeData, &fakeNodeInfo, logger, name, vendorVersion)
	assert.NotNil(t, err)

	// Failed setting up driver, name empty
	err = icDriver.SetupIBMCSIDriver(provider, mounter, statsUtil, &fakeNodeData, &fakeNodeInfo, logger, "", vendorVersion)
	assert.NotNil(t, err)
}

// TestSetupIBMCSIDriver_ControllerServerNoStunnel tests that stunnel manager
// is NOT initialized when running as controller server
func TestSetupIBMCSIDriver_ControllerServerNoStunnel(t *testing.T) {
	vendorVersion := "test-vendor-version-1.1.2"
	driver := "mydriver"

	// Creating test logger
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Create fake provider and mounter
	provider, _ := cloudProvider.NewFakeIBMCloudStorageProvider("", logger)
	mounter := mountManager.NewFakeNodeMounter()
	statsUtil := &MockStatUtils{}

	fakeNodeData := nodeMetadata.FakeNodeMetadata{}
	fakeNodeInfo := nodeInfo.FakeNodeInfo{}
	fakeNodeData.GetRegionReturns("testregion")
	fakeNodeData.GetZoneReturns("testzone")
	fakeNodeData.GetWorkerIDReturns("testworker")
	fakeNodeInfo.NewNodeMetadataReturns(&fakeNodeData, nil)

	testCases := []struct {
		name             string
		isNodeServer     string
		osType           string
		expectStunnelMgr bool
		description      string
	}{
		{
			name:             "Controller server mode - IS_NODE_SERVER=false",
			isNodeServer:     "false",
			osType:           "",
			expectStunnelMgr: false,
			description:      "Stunnel manager should NOT be initialized for controller server",
		},
		{
			name:             "Controller server mode - IS_NODE_SERVER not set",
			isNodeServer:     "",
			osType:           "",
			expectStunnelMgr: false,
			description:      "Stunnel manager should NOT be initialized when IS_NODE_SERVER is not set",
		},
		{
			name:             "Controller server mode - IS_NODE_SERVER=False (capital F)",
			isNodeServer:     "False",
			osType:           "",
			expectStunnelMgr: false,
			description:      "Stunnel manager should NOT be initialized for any value other than 'true'",
		},
		{
			name:             "Node server mode - IS_NODE_SERVER=true",
			isNodeServer:     "true",
			osType:           "RHCOS",
			expectStunnelMgr: true,
			description:      "Stunnel manager SHOULD be initialized for node server",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set environment variables
			if tc.isNodeServer != "" {
				t.Setenv("IS_NODE_SERVER", tc.isNodeServer)
			}
			if tc.osType != "" {
				t.Setenv("OS_TYPE", tc.osType)
			}

			// Create new driver instance for each test
			icDriver := GetIBMCSIDriver()

			// Setup the IBM CSI driver
			err := icDriver.SetupIBMCSIDriver(provider, mounter, statsUtil, &fakeNodeData, &fakeNodeInfo, logger, driver, vendorVersion)
			assert.Nil(t, err, "Expected no error but got: %v", err)

			// Verify stunnel manager state based on server type
			if tc.expectStunnelMgr {
				assert.NotNil(t, icDriver.ns.StunnelMgr, tc.description)
			} else {
				assert.Nil(t, icDriver.ns.StunnelMgr, tc.description)
			}

			// Verify node server is always initialized
			assert.NotNil(t, icDriver.ns, "Node server should always be initialized")
			assert.NotNil(t, icDriver.cs, "Controller server should always be initialized")
		})
	}
}
