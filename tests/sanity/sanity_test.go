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

// Package sanity ...
package sanity

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	nodeInfo "github.com/IBM/ibmcloud-volume-file-vpc/pkg/metadata/fake"
	"github.com/IBM/ibmcloud-volume-interface/config"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	providerError "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	sanity "github.com/kubernetes-csi/csi-test/v4/pkg/sanity"

	mountManager "github.com/IBM/ibm-csi-common/pkg/mountmanager"
	cloudProvider "github.com/IBM/ibmcloud-volume-file-vpc/pkg/ibmcloudprovider"
	nodeMetadata "github.com/IBM/ibmcloud-volume-file-vpc/pkg/metadata"

	csiConfig "github.com/IBM/ibm-vpc-file-csi-driver/config"
	csiDriver "github.com/IBM/ibm-vpc-file-csi-driver/pkg/ibmcsidriver"
)

const (
	// ProviderName ...
	ProviderName = provider.VolumeProvider(csiConfig.CSIProviderName)

	// VolumeType ...
	VolumeType = provider.VolumeType(csiConfig.CSIProviderVolumeType)

	// FakeNodeID
	FakeNodeID = "fake-node-id"
)

var (
	// Set up variables
	TempDir = "/tmp/csi"

	// CSIEndpoint ...
	CSIEndpoint = fmt.Sprintf("unix:%s/csi.sock", TempDir)

	// TargetPath ...
	// TargetPath = path.Join(TempDir, "target")
	TargetPath = "target"

	// StagePath ...
	//StagePath = path.Join(TempDir, "staging")
	StagePath = "staging"
)

func TestSanity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping sanity testing...")
	}

	os.Setenv("VPC_SUBNET_IDS", "subnet-id")

	// Create a fake CSI driver
	csiSanityDriver := initCSIDriverForSanity(t)

	//  Create the temp directory for fake sanity driver
	err := os.MkdirAll(TempDir, 0750)
	if err != nil {
		t.Fatalf("Failed to create sanity temp working dir %s: %v", TempDir, err)
	}
	defer func() {
		// Clean up tmp dir
		if err = os.RemoveAll(TempDir); err != nil {
			t.Fatalf("Failed to clean up sanity temp working dir %s: %v", TempDir, err)
		}
	}()

	go func() {
		csiSanityDriver.Run(CSIEndpoint)
	}()

	// Run sanity test
	config := sanity.TestConfig{
		TargetPath:               TargetPath,
		StagingPath:              StagePath,
		Address:                  CSIEndpoint,
		DialOptions:              []grpc.DialOption{grpc.WithInsecure()}, //nolint
		IDGen:                    &providerIDGenerator{},
		TestVolumeParametersFile: os.Getenv("SANITY_PARAMS_FILE"),
		TestVolumeSize:           10737418240, // i.e 10 GB
		CreateTargetDir: func(targetPath string) (string, error) {
			targetPath = path.Join(TempDir, targetPath)
			return targetPath, createTargetDir(targetPath)
		},
		CreateStagingDir: func(stagePath string) (string, error) {
			stagePath = path.Join(TempDir, stagePath)
			return stagePath, createTargetDir(stagePath)
		},
	}
	sanity.Test(t, config)
}

var _ sanity.IDGenerator = &providerIDGenerator{}

type providerIDGenerator struct {
}

func (v providerIDGenerator) GenerateUniqueValidVolumeID() string {
	return fmt.Sprintf("vol-uuid-test-vol-%s:access-point-1", uuid.New().String()[:10])
}

func (v providerIDGenerator) GenerateInvalidVolumeID() string {
	return "invalid-vol-id:access-point-1"
}

func (v providerIDGenerator) GenerateUniqueValidNodeID() string {
	return fmt.Sprintf("%s-%s", FakeNodeID, uuid.New().String()[:10])
}

func (v providerIDGenerator) GenerateInvalidNodeID() string {
	return "invalid-Node-ID"
}

func initCSIDriverForSanity(t *testing.T) *csiDriver.IBMCSIDriver {
	vendorVersion := "test-vendor-version-1.1.2"
	driver := "fakedriver"

	// Creating test logger
	logger, teardown := cloudProvider.GetTestLogger(t)
	defer teardown()
	csiSanityDriver := csiDriver.GetIBMCSIDriver()

	// Create fake provider and mounter
	provider, _ := NewFakeSanityCloudProvider("", logger)
	mounter := mountManager.NewFakeNodeMounter()

	statsUtil := &MockStatSanity{}

	// fake node metadata
	fakeNodeData := nodeMetadata.FakeNodeMetadata{}
	fakeNodeInfo := nodeInfo.FakeNodeInfo{}
	fakeNodeData.GetRegionReturns("testregion")
	fakeNodeData.GetZoneReturns("testzone")
	fakeNodeData.GetWorkerIDReturns("testworker")
	fakeNodeInfo.NewNodeMetadataReturns(&fakeNodeData, nil)

	// Setup the IBM CSI Driver
	err := csiSanityDriver.SetupIBMCSIDriver(provider, mounter, statsUtil, &fakeNodeData, &fakeNodeInfo, logger, driver, vendorVersion)
	if err != nil {
		t.Fatalf("Failed to setup IBM CSI Driver: %v", err)
	}

	return csiSanityDriver
}

// Fake State interface methods implementation for getting
type MockStatSanity struct {
}

// FSInfo ...
func (su *MockStatSanity) FSInfo(path string) (int64, int64, int64, int64, int64, int64, error) {
	return 1, 1, 1, 1, 1, 1, nil
}

func (su *MockStatSanity) IsDevicePathNotExist(devicePath string) bool {
	// return true if not matched
	return !strings.Contains(devicePath, TargetPath)
}

// FakeSanityCloudProvider Provider
type FakeSanityCloudProvider struct {
	ProviderName   string
	ProviderConfig *config.Config
	fakeSession    *fakeProviderSession
	ClusterID      string
}

var _ cloudProvider.CloudProviderInterface = &FakeSanityCloudProvider{}

// NewFakeSanityCloudProvider ...
func NewFakeSanityCloudProvider(configPath string, logger *zap.Logger) (*FakeSanityCloudProvider, error) {
	return &FakeSanityCloudProvider{ProviderName: "FakeSanityCloudProvider",
		ProviderConfig: &config.Config{VPC: &config.VPCProviderConfig{VPCBlockProviderName: "VPCFakeProvider"}},
		ClusterID:      "", fakeSession: newFakeProviderSession()}, nil
}

// GetProviderSession ...
func (ficp *FakeSanityCloudProvider) GetProviderSession(ctx context.Context, logger *zap.Logger) (provider.Session, error) {
	return ficp.fakeSession, nil
}

// GetConfig ...
func (ficp *FakeSanityCloudProvider) GetConfig() *config.Config {
	return ficp.ProviderConfig
}

// GetClusterID ...
func (ficp *FakeSanityCloudProvider) GetClusterID() string {
	return ficp.ClusterID
}

type fakeVolume struct {
	*provider.Volume
}

type fakeVolumeAccessPointResponse struct {
	*provider.VolumeAccessPointResponse
}

type fakeProviderSession struct {
	provider.DefaultVolumeProvider
	volumes            map[string]*fakeVolume
	volumeAccessPoints map[string]*fakeVolumeAccessPointResponse
	pub                map[string]string
	providerName       provider.VolumeProvider
	providerType       provider.VolumeType
}

func newFakeProviderSession() *fakeProviderSession {
	return &fakeProviderSession{
		volumes:            make(map[string]*fakeVolume),
		volumeAccessPoints: make(map[string]*fakeVolumeAccessPointResponse),
		pub:                make(map[string]string),
		providerName:       csiConfig.CSIProviderName,
		providerType:       csiConfig.CSIProviderVolumeType,
	}
}

//##############################################################################
// Following are the fake interface methods from open source common library
// If there is any changes in the interface in the libarary then these also need
// to validate and modify accordingly
//##############################################################################

func (c *fakeProviderSession) GetSubnetForVolumeAccessPoint(subnetRequest provider.SubnetRequest) (string, error) {
	return "subnet-id", nil
}

// ProviderName ...
func (c *fakeProviderSession) ProviderName() provider.VolumeProvider {
	return ProviderName
}

// Type returns the underlying volume type
func (c *fakeProviderSession) Type() provider.VolumeType {
	return VolumeType
}

func (c *fakeProviderSession) Close() {
	// Do nothing for now
}

// GetProviderDisplayName returns the provider name
func (c *fakeProviderSession) GetProviderDisplayName() provider.VolumeProvider {
	return ProviderName
}

// Volume operations
// Create the volume with authorization by passing required information in the volume object
func (c *fakeProviderSession) CreateVolume(volumeRequest provider.Volume) (*provider.Volume, error) {
	if volumeRequest.Name == nil || len(*volumeRequest.Name) == 0 {
		return nil, errors.New("no Volume name passed")
	}
	fakeVolume := &fakeVolume{
		Volume: &provider.Volume{
			VolumeID: fmt.Sprintf("vol-uuid-test-vol-%s", uuid.New().String()[:10]),
			Name:     volumeRequest.Name,
			Region:   volumeRequest.Region,
			Capacity: volumeRequest.Capacity,
			VPCVolume: provider.VPCVolume{
				VPCFileVolume: provider.VPCFileVolume{
					VolumeAccessPoints: &[]provider.VolumeAccessPoint{
						{
							ID: "Fake-ID",
						},
					},
				},
			},
		},
	}

	c.volumes[*volumeRequest.Name] = fakeVolume
	return fakeVolume.Volume, nil
}

// Create the volume Acesss Point with authorization by passing required information in the volume access point object
func (c *fakeProviderSession) CreateVolumeAccessPoint(volumeAccesspointReq provider.VolumeAccessPointRequest) (*provider.VolumeAccessPointResponse, error) {
	if len(volumeAccesspointReq.VolumeID) == 0 {
		return nil, errors.New("no volume ID passed")
	}

	fakeVolumeAccessPointResponse := &fakeVolumeAccessPointResponse{
		VolumeAccessPointResponse: &provider.VolumeAccessPointResponse{
			VolumeID:      volumeAccesspointReq.VolumeID,
			AccessPointID: "access-point-1",
			Status:        "Stable",
			MountPath:     "nsfserver:/abc/xyz",
			CreatedAt:     &time.Time{},
		},
	}
	c.volumeAccessPoints[volumeAccesspointReq.VolumeID] = fakeVolumeAccessPointResponse
	return fakeVolumeAccessPointResponse.VolumeAccessPointResponse, nil
}

// WaitForAttachVolume waits for the volume to be attached to the host
// Return error if wait is timed out OR there is other error
func (c *fakeProviderSession) WaitForCreateVolumeAccessPoint(volumeAccesspointReq provider.VolumeAccessPointRequest) (*provider.VolumeAccessPointResponse, error) {
	if len(volumeAccesspointReq.VolumeID) == 0 {
		return nil, errors.New("no volume ID passed")
	}

	return &provider.VolumeAccessPointResponse{
		VolumeID:      volumeAccesspointReq.VolumeID,
		AccessPointID: "access-point-1",
		Status:        "Stable",
		MountPath:     "nsfserver:/abc/xyz",
		CreatedAt:     &time.Time{},
	}, nil
}

func (c *fakeProviderSession) UpdateVolume(volumeRequest provider.Volume) error {
	return nil
}

// Create the volume from snapshot with snapshot tags
func (c *fakeProviderSession) CreateVolumeFromSnapshot(snapshot provider.Snapshot, tags map[string]string) (*provider.Volume, error) {
	return nil, nil
}

// Delete the volume
func (c *fakeProviderSession) DeleteVolume(vol *provider.Volume) error {
	for volName, f := range c.volumes {
		if f.Volume.VolumeID == vol.VolumeID {
			delete(c.volumes, volName)
			return nil
		}
	}
	erroMsg := providerError.Message{
		Code:        "FailedToDeleteVolume",
		Description: "Volume not found for deletion",
		Type:        providerError.DeletionFailed,
	}

	return erroMsg
}

// Get the volume by using ID  //
func (c *fakeProviderSession) GetVolume(id string) (*provider.Volume, error) {
	for _, f := range c.volumes {
		if f.Volume.VolumeID == id {
			return f.Volume, nil
		}
	}
	errorMsg := providerError.Message{
		Code:        "StorageFindFailedWithVolumeName",
		Description: "Volume not found by volume ID",
		Type:        providerError.RetrivalFailed,
	}
	return nil, errorMsg
}

// Get the volume by using Name
func (c *fakeProviderSession) GetVolumeByName(name string) (*provider.Volume, error) {
	for _, f := range c.volumes {
		if *f.Volume.Name == name {
			return f.Volume, nil
		}
	}
	errorMsg := providerError.Message{
		Code:        "StorageFindFailedWithVolumeName",
		Description: "Volume not found by name",
		Type:        providerError.RetrivalFailed,
	}
	return nil, errorMsg
}

// Get volume lists
func (c *fakeProviderSession) ListVolumes(limit int, start string, tags map[string]string) (*provider.VolumeList, error) {
	maxLimit := 100
	var respVolumesList = &provider.VolumeList{}
	errorMsg := providerError.Message{
		Code:        "StartVolumeIDNotFound",
		Description: "The volume ID specified in the start parameter of the list volume call could not be found.",
		Type:        providerError.InvalidRequest,
	}
	if start != "" {
		if _, ok := c.volumes[start]; !ok {
			return nil, errorMsg
		}
	}

	if limit == 0 {
		limit = 50
	} else if limit > maxLimit {
		limit = maxLimit
	}
	i := 1
	for _, f := range c.volumes {
		if i > limit {
			break
		}
		respVolumesList.Volumes = append(respVolumesList.Volumes, f.Volume)
		i++
	}
	return respVolumesList, nil
}

// Others
// GetVolumeByRequestID fetch the volume by request ID.
// Request Id is the one that is returned when volume is provsioning request is
// placed with Iaas provider.
func (c *fakeProviderSession) GetVolumeByRequestID(requestID string) (*provider.Volume, error) {
	return nil, nil
}

// AuthorizeVolume allows aceess to volume  based on given authorization
func (c *fakeProviderSession) AuthorizeVolume(volumeAuthorization provider.VolumeAuthorization) error {
	return nil
}

// GetAttachAttachment retirves the current status of given volume attach request
func (c *fakeProviderSession) GetVolumeAttachment(attachRequest provider.VolumeAttachmentRequest) (*provider.VolumeAttachmentResponse, error) {
	return nil, nil
}

func (c *fakeProviderSession) OrderSnapshot(VolumeRequest provider.Volume) error {
	return nil
}

// Snapshot operations
// Create the snapshot on the volume
func (c *fakeProviderSession) CreateSnapshot(sourceVolumeID string, snapshotParameters provider.SnapshotParameters) (*provider.Snapshot, error) {
	return nil, nil
}

// Delete the snapshot
func (c *fakeProviderSession) DeleteSnapshot(*provider.Snapshot) error {
	return nil
}

// Get the snapshot
func (c *fakeProviderSession) GetSnapshot(snapshotID string, sourceVolumeID ...string) (*provider.Snapshot, error) {
	return nil, nil
}

// Get the snapshot with volume ID
func (c *fakeProviderSession) GetSnapshotWithVolumeID(volumeID string, snapshotID string) (*provider.Snapshot, error) {
	return nil, nil
}

// Snapshot list by using tags
func (c *fakeProviderSession) ListSnapshots(limit int, start string, tags map[string]string) (*provider.SnapshotList, error) {
	return nil, nil
}

// List all the  snapshots for a given volume
func (c *fakeProviderSession) ListAllSnapshots(volumeID string) ([]*provider.Snapshot, error) {
	return nil, nil
}

func createTargetDir(targetPath string) error {
	fileInfo, err := os.Stat(targetPath)
	if err != nil && os.IsNotExist(err) {
		return os.MkdirAll(targetPath, 0755) //nolint
	} else if err != nil {
		return err
	}
	if !fileInfo.IsDir() {
		return fmt.Errorf("target location %s is not a directory", targetPath)
	}

	return nil
}
