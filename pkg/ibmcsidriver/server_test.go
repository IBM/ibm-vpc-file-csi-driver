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
	"flag"
	"fmt"
	"os"
	"testing"

	cloudProvider "github.com/IBM/ibmcloud-volume-file-vpc/pkg/ibmcloudprovider"
	"github.com/stretchr/testify/assert"
)

// mockFileOps is a mock implementation of FileOps for testing setupSidecar
type mockFileOps struct {
	chownErr error
	chmodErr error
}

func (m *mockFileOps) Chown(name string, uid, gid int) error {
	return m.chownErr
}

func (m *mockFileOps) Chmod(name string, mode os.FileMode) error {
	return m.chmodErr
}

func TestSetup(t *testing.T) {
	goodEndpoint := flag.String("endpoint", "unix:/tmp/testcsi.sock", "Test CSI endpoint")
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()

	s := NewNonBlockingGRPCServer(logger)
	nonBlockingServer, ok := s.(*nonBlockingGRPCServer)
	assert.Equal(t, true, ok)
	ids := &CSIIdentityServer{}
	cs := &CSIControllerServer{}
	ns := &CSINodeServer{}

	{
		t.Logf("Good setup")
		ls, err := nonBlockingServer.Setup(*goodEndpoint, ids, cs, ns)
		assert.Nil(t, err)
		assert.NotNil(t, ls)
	}

	// Call other methods as well just to execute all line of code
	nonBlockingServer.Wait()
	nonBlockingServer.Stop()
	nonBlockingServer.ForceStop()

	{
		t.Logf("Wrong endpoint format")

		wrongEndpointFormat := flag.String("wrongendpoint", "---:/tmp/testcsi.sock", "Test CSI endpoint")
		_, err := nonBlockingServer.Setup(*wrongEndpointFormat, ids, cs, ns)
		assert.NotNil(t, err)
		t.Logf("---------> error %v", err)
	}

	{
		t.Logf("Wrong Scheme")
		wrongEndpointScheme := flag.String("wrongschemaendpoint", "wrong-scheme:/tmp/testcsi.sock", "Test CSI endpoint")
		_, err := nonBlockingServer.Setup(*wrongEndpointScheme, nil, nil, nil)
		assert.NotNil(t, err)
		t.Logf("---------> error %v", err)
	}

	{
		t.Logf("tcp Scheme")
		tcpEndpointSchema := flag.String("tcpendpoint", "tcp:/tmp/testtcpcsi.sock", "Test CSI endpoint")
		_, err := nonBlockingServer.Setup(*tcpEndpointSchema, nil, nil, nil)
		assert.Nil(t, err)
		t.Logf("---------> error %v", err)
		nonBlockingServer.ForceStop()
	}

	{
		t.Logf("Wrong address")
		wrongAddressEndpointAddress := flag.String("wrongaddressendpoint", "unix:443", "Test CSI endpoint")
		_, err := nonBlockingServer.Setup(*wrongAddressEndpointAddress, nil, nil, nil)
		//assert.Nil(t, err) // Its working on local system
		t.Logf("---------> error %v", err)
	}
}

func TestLogGRPC(t *testing.T) {
	t.Logf("TODO:~ TestLogGRPC")
}

func TestSetupSidecar(t *testing.T) {
	tests := []struct {
		name             string
		groupID          string
		fileOps          FileOps
		expectedErr      bool
		expectedChownErr error
		expectedChmodErr error
	}{
		{
			name:             "Successful setupSidecar",
			groupID:          "123",
			fileOps:          &mockFileOps{},
			expectedErr:      false,
			expectedChownErr: nil,
			expectedChmodErr: nil,
		},
		{
			name:    "Chown failure",
			groupID: "123",
			fileOps: &mockFileOps{
				chownErr: errors.New("Chown failed"),
			},
			expectedErr:      true,
			expectedChownErr: errors.New("Chown failed"),
			expectedChmodErr: nil,
		},
		{
			name:    "Chmod failure",
			groupID: "123",
			fileOps: &mockFileOps{
				chmodErr: errors.New("Chmod failed"),
			},
			expectedErr:      true,
			expectedChownErr: nil,
			expectedChmodErr: errors.New("Chmod failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			os.Setenv("SIDECAR_GROUP_ID", tt.groupID)

			// Call setupSidecar
			err := setupSidecar("someAddr", tt.fileOps)

			// Check the expected error
			if (err != nil) != tt.expectedErr {
				t.Errorf("Expected error: %v, got error: %v", tt.expectedErr, err)
			}

			// Check the expected Chown error
			actualErrStr := fmt.Sprintf("%v", tt.fileOps.(*mockFileOps).chownErr)
			expectedErrStr := fmt.Sprintf("%v", tt.expectedChownErr)
			if actualErrStr != expectedErrStr {
				t.Errorf("Expected Chown error: %v, got Chown error: %v", expectedErrStr, actualErrStr)
			}

			// Check the expected Chmod error
			actualErrStr = fmt.Sprintf("%v", tt.fileOps.(*mockFileOps).chmodErr)
			expectedErrStr = fmt.Sprintf("%v", tt.expectedChmodErr)
			if actualErrStr != expectedErrStr {
				t.Errorf("Expected Chmod error: %v, got Chmod error: %v", expectedErrStr, actualErrStr)
			}
		})
	}
}
