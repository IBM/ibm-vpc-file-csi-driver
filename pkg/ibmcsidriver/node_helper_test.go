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
	"testing"

	commonError "github.com/IBM/ibm-csi-common/pkg/messages"
	cloudProvider "github.com/IBM/ibmcloud-volume-file-vpc/pkg/ibmcloudprovider"
	"github.com/stretchr/testify/assert"
)

func TestProcessMount(t *testing.T) {
	// Creating test logger
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	icDriver := initIBMCSIDriver(t)
	ops := []string{"a", "b"}
	response, err := icDriver.ns.processMount(logger, "processMount", "/staging", "/targetpath", "ext4", "", ops)
	t.Logf("Response %v, error %v", response, err)

	response, err = icDriver.ns.processMount(logger, "processMount", "/staging", "/targetpath", "ibmshare", "ipsec", ops)
	t.Logf("Response %v, error %v", response, err)
}

func TestCheckMountResponse(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "Test MetadataServiceNotEnabled",
			err:      errors.New("exit status 1"),
			expected: commonError.MetadataServiceNotEnabled,
		},
		{
			name:     "Test UnresponsiveMountHelperContainerUtility",
			err:      errors.New("connect: no such file"),
			expected: commonError.UnresponsiveMountHelperContainerUtility,
		},
		{
			name:     "Test Default",
			err:      errors.New("some other error"),
			expected: commonError.MountingTargetFailed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := checkMountResponse(tc.err)
			assert.Equal(t, tc.expected, result)
		})
	}
}
