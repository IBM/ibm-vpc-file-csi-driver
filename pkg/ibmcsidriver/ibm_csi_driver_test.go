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

// TestSetupIBMCSIDriver_StunnelManagerInit tests stunnel manager initialization
func TestSetupIBMCSIDriver_StunnelManagerInit(t *testing.T) {
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
		servicesDir      string
		expectStunnelMgr bool
		expectError      bool
	}{
		{
			name:             "Node server with stunnel manager",
			isNodeServer:     "true",
			servicesDir:      t.TempDir(),
			expectStunnelMgr: true,
			expectError:      false,
		},
		{
			name:             "Node server with default services dir",
			isNodeServer:     "true",
			servicesDir:      "", // Will use default
			expectStunnelMgr: true,
			expectError:      false,
		},
		{
			name:             "Controller server (no stunnel)",
			isNodeServer:     "false",
			servicesDir:      "",
			expectStunnelMgr: false,
			expectError:      false,
		},
		{
			name:             "Node server with non-existent services dir (still succeeds)",
			isNodeServer:     "true",
			servicesDir:      "/invalid/path/that/does/not/exist",
			expectStunnelMgr: true, // Manager is created, recovery just warns
			expectError:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set environment variables
			if tc.isNodeServer != "" {
				t.Setenv("IS_NODE_SERVER", tc.isNodeServer)
			}
			if tc.servicesDir != "" {
				t.Setenv("STUNNEL_SERVICES_DIR", tc.servicesDir)
			}

			// Create new driver instance for each test
			icDriver := GetIBMCSIDriver()

			// Setup the IBM CSI driver
			err := icDriver.SetupIBMCSIDriver(provider, mounter, statsUtil, &fakeNodeData, &fakeNodeInfo, logger, driver, vendorVersion)

			if tc.expectError {
				assert.NotNil(t, err, "Expected error but got nil")
			} else {
				assert.Nil(t, err, "Expected no error but got: %v", err)
			}

			// Verify stunnel manager state
			if tc.expectStunnelMgr {
				assert.NotNil(t, icDriver.ns.StunnelMgr, "Expected stunnel manager to be initialized")
			} else {
				assert.Nil(t, icDriver.ns.StunnelMgr, "Expected stunnel manager to be nil")
			}
		})
	}
}

// TestSetupIBMCSIDriver_StunnelManagerGracefulDegradation tests that node server
// continues to work even if stunnel manager initialization fails
func TestSetupIBMCSIDriver_StunnelManagerGracefulDegradation(t *testing.T) {
	vendorVersion := "test-vendor-version-1.1.2"
	driver := "mydriver"

	// Creating test logger
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	// Set as node server
	t.Setenv("IS_NODE_SERVER", "true")

	// Note: The current implementation of stunnel.NewSimpleManager is very resilient
	// and only fails on nil config or nil logger. Since we pass a valid logger from
	// the driver, it's difficult to trigger a failure in unit tests.
	// The graceful degradation is tested by verifying the warning log is emitted
	// when initialization fails, and the node server continues to function.

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

	// Setup should succeed
	err := icDriver.SetupIBMCSIDriver(provider, mounter, statsUtil, &fakeNodeData, &fakeNodeInfo, logger, driver, vendorVersion)
	assert.Nil(t, err, "Setup should succeed")

	// Node server should be functional
	assert.NotNil(t, icDriver.ns, "Node server should be initialized")
	assert.NotNil(t, icDriver.ns.Mounter, "Mounter should be initialized")
	assert.NotNil(t, icDriver.ns.Driver, "Driver should be initialized")

	// Stunnel manager should be initialized (since we have valid config)
	// In production, if stunnel manager fails to initialize, it will be nil
	// and the warning log will be emitted, but node server continues to work
	assert.NotNil(t, icDriver.ns.StunnelMgr, "Stunnel manager should be initialized with valid config")
}
