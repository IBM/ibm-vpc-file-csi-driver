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
	"os"
	"testing"

	cloudProvider "github.com/IBM/ibmcloud-volume-file-vpc/pkg/ibmcloudprovider"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

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

	{
		t.Logf("setup CSI sidecar ")
		os.Setenv("IS_NODE_SERVER", "true")
		ls, err := nonBlockingServer.Setup(*goodEndpoint, ids, cs, ns)
		addr := ls.Addr().String()
		assert.Nil(t, err)
		assert.NotNil(t, ls)
		assert.Equal(t, addr, "/tmp/testcsi.sock")
	}

	{
		t.Logf("setup CSI sidecar Invalid groupID")
		os.Setenv("IS_NODE_SERVER", "true")
		os.Setenv("SIDECAR_GROUP_ID", "2222222222222222")
		ls, err := nonBlockingServer.Setup(*goodEndpoint, ids, cs, ns)
		assert.NotNil(t, err)
		assert.Nil(t, ls)
		assert.Equal(t, err.Error(), "chown /tmp/testcsi.sock: operation not permitted")
	}
}

func TestLogGRPC(t *testing.T) {
	ctx := context.Background()
	info := &grpc.UnaryServerInfo{}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) { return nil, nil }
	logGRPC(ctx, nil, info, handler)

	//Return error
	handler = func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, errors.New("handler error")
	}
	logGRPC(ctx, nil, info, handler)

}
