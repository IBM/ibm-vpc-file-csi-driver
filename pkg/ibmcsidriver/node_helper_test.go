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
	"os"
	"testing"

	mountManager "github.com/IBM/ibm-csi-common/pkg/mountmanager"
	commonError "github.com/IBM/ibm-csi-common/pkg/messages"
	cloudProvider "github.com/IBM/ibmcloud-volume-file-vpc/pkg/ibmcloudprovider"
	"github.com/stretchr/testify/assert"
)

// mounterWithMountError wraps a real Mounter and overrides Mount to return
// a configurable error, allowing tests to exercise the mount-failure path in
// processMount without depending on real OS mount behaviour.
type mounterWithMountError struct {
	mountManager.Mounter
	mountErr error
}

func (m *mounterWithMountError) Mount(source, target, fsType string, options []string) error {
	return m.mountErr
}

// mounterWithEITError wraps a real Mounter and overrides MountEITBasedFileShare
// to return a configurable error, for testing the EIT mount-failure path.
type mounterWithEITError struct {
	mountManager.Mounter
	mountErr error
}

func (m *mounterWithEITError) MountEITBasedFileShare(mountPath, targetPath, fsType, transitEncryption, requestID string) (string, error) {
	return "", m.mountErr
}

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

func TestProcessMount_MountFailure(t *testing.T) {
	tests := []struct {
		name            string
		fsType          string
		transitEnc      string
		mountErr        error
		expectedErrCode string
		// targetInTempDir causes the targetPath to be a real temp directory so
		// that os.Remove succeeds (exercising MountingTargetFailed).
		// When false the targetPath is a non-existent path so os.Remove fails
		// (exercising CreateMountTargetFailed).
		targetInTempDir bool
	}{
		{
			name:            "Non-EIT mount failure: MountingTargetFailed when Remove succeeds",
			fsType:          "nfs",
			mountErr:        errors.New("exit status 32: Protocol not supported"),
			expectedErrCode: commonError.MountingTargetFailed,
			targetInTempDir: true,
		},
		{
			name:            "Non-EIT mount failure: CreateMountTargetFailed when Remove fails",
			fsType:          "nfs",
			mountErr:        errors.New("exit status 32: Protocol not supported"),
			expectedErrCode: commonError.CreateMountTargetFailed,
			targetInTempDir: false,
		},
		{
			name:            "EIT mount failure: exit status 1 -> MetadataServiceNotEnabled",
			fsType:          eitFsType,
			transitEnc:      "ipsec",
			mountErr:        errors.New("exit status 1"),
			expectedErrCode: commonError.MetadataServiceNotEnabled,
			targetInTempDir: true,
		},
		{
			name:            "EIT mount failure: connect no such file -> UnresponsiveMountHelperContainerUtility",
			fsType:          eitFsType,
			transitEnc:      "ipsec",
			mountErr:        errors.New("connect: no such file"),
			expectedErrCode: commonError.UnresponsiveMountHelperContainerUtility,
			targetInTempDir: true,
		},
		{
			name:            "EIT mount failure: unknown error -> MountingTargetFailed",
			fsType:          eitFsType,
			transitEnc:      "ipsec",
			mountErr:        errors.New("some unexpected EIT error"),
			expectedErrCode: commonError.MountingTargetFailed,
			targetInTempDir: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger, teardown := cloudProvider.GetTestLogger(t)
			defer teardown()

			icDriver := initIBMCSIDriver(t)

			// Choose and configure the appropriate mount-error injector.
			var mounter mountManager.Mounter
			if tc.fsType == eitFsType {
				mounter = &mounterWithEITError{
					Mounter:  icDriver.ns.Mounter,
					mountErr: tc.mountErr,
				}
			} else {
				mounter = &mounterWithMountError{
					Mounter:  icDriver.ns.Mounter,
					mountErr: tc.mountErr,
				}
			}
			icDriver.ns.Mounter = mounter

			// Determine targetPath: a real directory (Remove will succeed) or a
			// path that does not exist so that Remove returns an error.
			var targetPath string
			if tc.targetInTempDir {
				// MakeDir is handled by the fake mounter; we only need the path
				// to exist so that os.Remove inside processMount can clean it up.
				targetPath = t.TempDir()
				// processMount calls MakeDir which is a no-op in the fake, so
				// the directory already exists from t.TempDir().
			} else {
				// Use a path that will never exist so os.Remove fails.
				targetPath = "/nonexistent-path-for-test-" + tc.name
				// Ensure it really doesn't exist.
				_ = os.Remove(targetPath)
			}

			// Use an existing TempDir — that path already exists and we want Remove to
			// find it after the mount fails.
			resp, err := icDriver.ns.processMount(logger, "req-001", "10.0.0.1:/vol", targetPath, tc.fsType, tc.transitEnc, nil)

			assert.Nil(t, resp)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.expectedErrCode)
		})
	}
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
