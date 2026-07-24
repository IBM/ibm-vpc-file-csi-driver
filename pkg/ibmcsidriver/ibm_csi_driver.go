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
	"context"
	"fmt"
	"os"

	commonError "github.com/IBM/ibm-csi-common/pkg/messages"
	mountManager "github.com/IBM/ibm-csi-common/pkg/mountmanager"
	"github.com/IBM/ibm-csi-common/pkg/utils"
	"github.com/IBM/ibm-vpc-file-csi-driver/pkg/rfseit"
	fileprovider "github.com/IBM/ibmcloud-volume-file-vpc/file/provider"
	cloudProvider "github.com/IBM/ibmcloud-volume-file-vpc/pkg/ibmcloudprovider"
	nodeMetadata "github.com/IBM/ibmcloud-volume-file-vpc/pkg/metadata"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"go.uber.org/zap"
)

// IBMCSIDriver ...
type IBMCSIDriver struct {
	name          string
	vendorVersion string
	logger        *zap.Logger
	region        string
	rfsEnabled    bool

	ids *CSIIdentityServer
	ns  *CSINodeServer
	cs  *CSIControllerServer

	vcap  []*csi.VolumeCapability_AccessMode
	cscap []*csi.ControllerServiceCapability
	nscap []*csi.NodeServiceCapability
}

// GetIBMCSIDriver ...
func GetIBMCSIDriver() *IBMCSIDriver {
	return &IBMCSIDriver{}
}

// SetupIBMCSIDriver ...
func (icDriver *IBMCSIDriver) SetupIBMCSIDriver(provider cloudProvider.CloudProviderInterface, mounter mountManager.Mounter, statsUtil StatsUtils, metadata nodeMetadata.NodeMetadata, nodeInfo nodeMetadata.NodeInfo, lgr *zap.Logger, name, vendorVersion string) error {
	icDriver.logger = lgr
	icDriver.logger.Info("IBMCSIDriver-SetupIBMCSIDriver setting up IBM CSI Driver...")

	if provider == nil {
		return fmt.Errorf("provider not initialized")
	}

	if mounter == nil {
		return fmt.Errorf("mounter not initialized")
	}

	if name == "" {
		return fmt.Errorf("driver name missing")
	}

	// Setup messaging
	commonError.MessagesEn = commonError.InitMessages()

	//icDriver.provider = provider
	icDriver.name = name
	icDriver.vendorVersion = vendorVersion

	// Adding Capabilities
	vcam := []csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,      // RWO
		csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER, // RWX
	}

	_ = icDriver.AddVolumeCapabilityAccessModes(vcam) // #nosec G104: Attempt to AddVolumeCapabilityAccessModes only on best-effort basis. Error cannot be usefully handled.
	csc := []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		//csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		// csi.ControllerServiceCapability_RPC_GET_CAPACITY,
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
		// csi.ControllerServiceCapability_RPC_PUBLISH_READONLY,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
	}
	_ = icDriver.AddControllerServiceCapabilities(csc) // #nosec G104: Attempt to AddControllerServiceCapabilities only on best-effort basis. Error cannot be usefully handled.

	ns := []csi.NodeServiceCapability_RPC_Type{
		//csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
		csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
		//csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
	}
	_ = icDriver.AddNodeServiceCapabilities(ns) // #nosec G104: Attempt to AddNodeServiceCapabilities only on best-effort basis. Error cannot be usefully handled.

	// Set up CSI RPC Servers
	icDriver.ids = NewIdentityServer(icDriver)
	icDriver.ns = NewNodeServer(icDriver, mounter, statsUtil, metadata)

	// Fetch dp2 catalog bands once at startup and build a CapacityRoundoff
	// service. This is the driver's caching point: the returned value is stored
	// on CSIControllerServer for the pod lifetime. StorageClasses that set
	// allowCapacityRoundoffForIops=true will return a clear error at PVC
	// creation time if the catalog was unavailable here.
	var catalogProvider fileprovider.CapacityRoundoff
	bands, catalogErr := fileprovider.FetchCapacityBandsDP2(nil)
	if catalogErr != nil {
		lgr.Warn("Failed to fetch dp2 catalog bands; allowCapacityRoundoffForIops will return an error at PVC creation time",
			zap.Error(catalogErr))
	} else {
		catalogProvider, catalogErr = fileprovider.NewCapacityRoundoff(bands)
		if catalogErr != nil {
			lgr.Warn("Failed to build capacity roundoff service from dp2 bands; allowCapacityRoundoffForIops will return an error at PVC creation time",
				zap.Error(catalogErr))
		}
	}

	icDriver.cs = NewControllerServer(icDriver, provider, catalogProvider)

	icDriver.logger.Info("Successfully setup IBM CSI driver")

	// Set up Region
	regionMetadata, err := nodeInfo.NewNodeMetadata(lgr)
	if err != nil {
		return fmt.Errorf("Controller_Helper: Failed to initialize node metadata: error: %v", err)
	}
	icDriver.region = regionMetadata.GetRegion()

	// get the session
	icDriver.rfsEnabled = false
	session, err := provider.GetProviderSession(context.Background(), lgr)
	if err != nil {
		icDriver.logger.Warn("Cannot fetch session for verifying RFS profile")
		return nil
	}

	_, err = session.GetVolumeProfileByName(RFSProfile)
	if err != nil {
		icDriver.logger.Warn("RFS Profile is not accessible, please open support ticket on VPC for allowlisting. Restart of VPC FILE CSI Driver is required post allowlisting")
	} else {
		icDriver.rfsEnabled = true
		icDriver.logger.Info("RFS profile is supported")
	}

	// Initialize tunnel manager gRPC client only for node servers (not controllers)
	// Initialize stunnel manager for node server (works with stunnel sidecar)
	if os.Getenv("IS_NODE_SERVER") == "true" {
		// Create simple stunnel manager with hardcoded defaults
		stunnelMgr, err := rfseit.NewStunnelManager(icDriver.logger)
		if err != nil {
			// Enhanced error logging with troubleshooting guidance
			if icDriver.rfsEnabled {
				// RFS is enabled - stunnel manager failure will cause RFS EIT mount failures
				icDriver.logger.Warn("Failed to create stunnel manager - RFS EIT mounts will FAIL",
					zap.Error(err),
					zap.Bool("rfsEnabled", true),
					zap.String("impact", "All RFS EIT profile mounts will fail at mount time"),
					zap.String("action", "Check: 1) OS_TYPE env var is set correctly, 2) CLUSTER_ENV is set, 3) CA bundle file exists, 4) Restart node server pod to retry"))
			} else {
				// RFS not enabled - only log warning
				icDriver.logger.Warn("Failed to create stunnel manager - RFS EIT mounts will not work",
					zap.Error(err),
					zap.Bool("rfsEnabled", false),
					zap.String("note", "Node server will continue - non-RFS mounts unaffected"))
			}
			icDriver.ns.StunnelMgr = nil
		} else {
			icDriver.ns.StunnelMgr = stunnelMgr
			icDriver.logger.Info("Successfully initialized stunnel manager for node server with hardcoded defaults",
				zap.String("servicesDir", rfseit.DefaultServicesDir),
				zap.Int("basePort", rfseit.InitialPort),
				zap.Int("portRange", rfseit.PortRange),
				zap.Bool("rfsEnabled", icDriver.rfsEnabled),
				zap.String("note", "Works with stunnel sidecar container"))
		}
	} else {
		icDriver.logger.Info("Skipping stunnel manager initialization (running as controller)")
	}

	return nil
}

// AddVolumeCapabilityAccessModes ...
func (icDriver *IBMCSIDriver) AddVolumeCapabilityAccessModes(vc []csi.VolumeCapability_AccessMode_Mode) error {
	icDriver.logger.Info("IBMCSIDriver-AddVolumeCapabilityAccessModes...", zap.Reflect("VolumeCapabilityAccessModes", vc))
	var vca []*csi.VolumeCapability_AccessMode
	for _, c := range vc {
		icDriver.logger.Info("Enabling volume access mode", zap.Reflect("Mode", c.String()))
		vca = append(vca, utils.NewVolumeCapabilityAccessMode(c))
	}
	icDriver.vcap = vca
	icDriver.logger.Info("Successfully enabled Volume Capability Access Modes")
	return nil
}

// AddControllerServiceCapabilities ...
func (icDriver *IBMCSIDriver) AddControllerServiceCapabilities(cl []csi.ControllerServiceCapability_RPC_Type) error {
	icDriver.logger.Info("IBMCSIDriver-AddControllerServiceCapabilities...", zap.Reflect("ControllerServiceCapabilities", cl))
	var csc []*csi.ControllerServiceCapability
	for _, c := range cl {
		icDriver.logger.Info("Adding controller service capability", zap.Reflect("Capability", c.String()))
		csc = append(csc, utils.NewControllerServiceCapability(c))
	}
	icDriver.cscap = csc
	icDriver.logger.Info("Successfully added Controller Service Capabilities")
	return nil
}

// AddNodeServiceCapabilities ...
func (icDriver *IBMCSIDriver) AddNodeServiceCapabilities(nl []csi.NodeServiceCapability_RPC_Type) error {
	icDriver.logger.Info("IBMCSIDriver-AddNodeServiceCapabilities...", zap.Reflect("NodeServiceCapabilities", nl))
	var nsc []*csi.NodeServiceCapability
	for _, n := range nl {
		icDriver.logger.Info("Adding node service capability", zap.Reflect("NodeServiceCapabilities", n.String()))
		nsc = append(nsc, utils.NewNodeServiceCapability(n))
	}
	icDriver.nscap = nsc
	icDriver.logger.Info("Successfully added Node Service Capabilities")
	return nil
}

// ValidateControllerServiceRequest ...
/*func (icDriver *IBMCSIDriver) ValidateControllerServiceRequest(c csi.ControllerServiceCapability_RPC_Type) error {
	icDriver.logger.Info("In Driver's ValidateControllerServiceRequest ...", zap.Reflect("ControllerServiceRequest", c))
	if c == csi.ControllerServiceCapability_RPC_UNKNOWN {
		return nil
	}

	for _, cap := range icDriver.cscap {
		if c == cap.GetRpc().Type {
			return nil
		}
	}

	return status.Error(codes.InvalidArgument, "Invalid controller service request")
}*/

// NewIdentityServer ...
func NewIdentityServer(icDriver *IBMCSIDriver) *CSIIdentityServer {
	return &CSIIdentityServer{
		Driver: icDriver,
	}
}

// NewNodeServer ...
func NewNodeServer(icDriver *IBMCSIDriver, mounter mountManager.Mounter, statsUtil StatsUtils, nodeMetadata nodeMetadata.NodeMetadata) *CSINodeServer {
	return &CSINodeServer{
		Driver:     icDriver,
		Mounter:    mounter,
		Stats:      statsUtil,
		Metadata:   nodeMetadata,
		StunnelMgr: nil, // Will be initialized in SetupIBMCSIDriver if IS_NODE_SERVER=true
	}
}

// NewControllerServer ...
func NewControllerServer(icDriver *IBMCSIDriver, provider cloudProvider.CloudProviderInterface, catalogProvider fileprovider.CapacityRoundoff) *CSIControllerServer {
	return &CSIControllerServer{
		Driver:          icDriver,
		CSIProvider:     provider,
		CatalogProvider: catalogProvider,
	}
}

// Run ...
func (icDriver *IBMCSIDriver) Run(endpoint string) {
	icDriver.logger.Info("IBMCSIDriver-Run...", zap.Reflect("Endpoint", endpoint))
	icDriver.logger.Info("CSI Driver Name", zap.Reflect("Name", icDriver.name))

	//Start the nonblocking GRPC
	s := NewNonBlockingGRPCServer(icDriver.logger)
	// TODO(#34): Only start specific servers based on a flag.
	// In the future have this only run specific combinations of servers depending on which version this is.
	// The schema for that was in util. basically it was just s.start but with some nil servers.

	s.Start(endpoint, icDriver.ids, icDriver.cs, icDriver.ns)
	s.Wait()
}
