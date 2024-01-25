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

	cloudProvider "github.com/IBM/ibmcloud-volume-file-vpc/pkg/ibmcloudprovider"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/status"
)

func TestProcessMount(t *testing.T) {
	// test cases
	testCases := []struct {
		name        string
		source      string
		targetPath  string
		fsType      string
		options     []string
		expectedErr string
	}{
		{
			name:        "success",
			source:      "/source",
			targetPath:  "/targetpath-fdsfdsfdf~~!@@@",
			fsType:      "nfs",
			options:     []string{"ro,sync"},
			expectedErr: "",
		},
		{
			name:        "Make directory fails",
			source:      "/source",
			targetPath:  "invalid-volPath-dir",
			fsType:      "nfs",
			options:     []string{"ro,sync"},
			expectedErr: "{RequestID: processMount, Code: TargetPathCreateFailed, Description: Failed to create target path 'invalid-volPath-dir', BackendError: Path Creation failed, Action: Please check if there is any error in POD describe related with volume attach}",
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
		// Call processMount
		_, err := icDriver.ns.processMount(logger, "processMount", tc.source, tc.targetPath, tc.fsType, tc.options)
		if tc.expectedErr != "" && err != nil {
			t.Logf("Error code")
			assert.NotNil(t, err)
			serverError, _ := status.FromError(err)
			assert.Equal(t, tc.expectedErr, serverError.Message())
		}
	}
}
