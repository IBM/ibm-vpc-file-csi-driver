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
	"testing"

	cloudProvider "github.com/IBM/ibmcloud-volume-file-vpc/pkg/ibmcloudprovider"
	"github.com/stretchr/testify/assert"
)

func TestProcessMount(t *testing.T) {
	// test cases
	testCases := []struct {
		name        string
		source      string
		targetPath  string
		fsType      string
		options     []string
		expectedErr error
	}{
		{
			name:        "success",
			source:      "/source",
			targetPath:  "/targetpath",
			fsType:      "nfs",
			options:     []string{"ro,sync"},
			expectedErr: nil,
		},
		{
			name:        "Invalid source path",
			source:      "./error_mount_source",
			targetPath:  "/targetpath",
			fsType:      "nfs",
			options:     []string{"ro,sync"},
			expectedErr: fmt.Errorf("fake Mount: source error"),
		},
		{
			name:        "Invalid target path",
			source:      "./error_mount_target",
			targetPath:  "fake-volPath",
			fsType:      "nfs",
			options:     []string{"ro,sync"},
			expectedErr: fmt.Errorf("fake Mount: target error"),
		},
		{
			name:        "Make directory fails",
			source:      "/source",
			targetPath:  "invalid-volPath-dir",
			fsType:      "nfs",
			options:     []string{"ro,sync"},
			expectedErr: fmt.Errorf("Path Creation failed"),
		},
		{
			name:        "IsLikelyNotMountPoint returns true and nil error",
			source:      "./error_mount_source",
			targetPath:  "fake-volPath-1",
			fsType:      "nfs",
			options:     []string{"ro,sync"},
			expectedErr: fmt.Errorf("fake Mount: target error"),
		},
		{
			name:        "Umount Fails",
			source:      "./error_mount_source",
			targetPath:  "error_umount",
			fsType:      "nfs",
			options:     []string{"ro,sync"},
			expectedErr: fmt.Errorf("Unmount Failed"),
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
		// Call processMound
		_, err := icDriver.ns.processMount(logger, "processMount", tc.source, tc.targetPath, tc.fsType, tc.options)
		if tc.expectedErr != nil {
			t.Logf("Error code")
			assert.NotNil(t, err)
		}
	}
}
