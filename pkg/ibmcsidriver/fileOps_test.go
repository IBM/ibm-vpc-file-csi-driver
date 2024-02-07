/**
 *
 * Copyright 2024- IBM Inc. All rights reserved
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock implementation of the socketPermission interface
type mockSocketPermission struct {
	mock.Mock
}

func (m *mockSocketPermission) chown(name string, uid, gid int) error {
	args := m.Called(name, uid, gid)
	return args.Error(0)
}

func (m *mockSocketPermission) chmod(name string, mode os.FileMode) error {
	args := m.Called(name, mode)
	return args.Error(0)
}

func TestSetupSidecar(t *testing.T) {
	tests := []struct {
		name               string
		socketPermission   socketPermission
		groupID            string
		expectedErr        bool
		chownErr           error
		chmodErr           error
		expectedChownCalls int
		expectedChmodCalls int
	}{
		{
			name:               "ValidGroupID",
			socketPermission:   &mockSocketPermission{},
			groupID:            "2121",
			expectedErr:        false,
			chownErr:           nil,
			chmodErr:           nil,
			expectedChownCalls: 1,
			expectedChmodCalls: 1,
		},
		{
			name:               "EmptyGroupID",
			socketPermission:   &mockSocketPermission{},
			groupID:            "",
			expectedErr:        false,
			chownErr:           nil,
			chmodErr:           nil,
			expectedChownCalls: 1,
			expectedChmodCalls: 1,
		},
		{
			name:               "ChownError",
			socketPermission:   &mockSocketPermission{},
			groupID:            "1000",
			expectedErr:        true,
			chownErr:           errors.New("chown error"),
			chmodErr:           nil,
			expectedChownCalls: 1,
			expectedChmodCalls: 0, // No chmod expected if chown fails
		},
		{
			name:               "ChmodError",
			socketPermission:   &mockSocketPermission{},
			groupID:            "1000",
			expectedErr:        true,
			chownErr:           nil,
			chmodErr:           errors.New("chmod error"),
			expectedChownCalls: 1,
			expectedChmodCalls: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set the environment variable
			os.Setenv("SIDECAR_GROUP_ID", tc.groupID)
			defer os.Unsetenv("SIDECAR_GROUP_ID")

			// Create mock object
			mockSocketPermission := tc.socketPermission.(*mockSocketPermission)

			// Set expectations for chown and chmod methods
			mockSocketPermission.On("chown", mock.Anything, -1, mock.AnythingOfType("int")).Return(tc.chownErr).Times(tc.expectedChownCalls)
			mockSocketPermission.On("chmod", mock.Anything, os.FileMode(filePermission)).Return(tc.chmodErr).Times(tc.expectedChmodCalls)

			err := setupSidecar("/path/to/socket", mockSocketPermission)

			if tc.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
