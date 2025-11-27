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
	"math/rand"
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

	_ = os.Setenv("VPC_SUBNET_IDS", "subnet-id")

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
func (su *MockStatSanity) FSInfo(_ string) (int64, int64, int64, int64, int64, int64, error) {
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
func NewFakeSanityCloudProvider(_ string, _ *zap.Logger) (*FakeSanityCloudProvider, error) {
	return &FakeSanityCloudProvider{ProviderName: "FakeSanityCloudProvider",
		ProviderConfig: &config.Config{VPC: &config.VPCProviderConfig{VPCBlockProviderName: "VPCFakeProvider"}},
		ClusterID:      "", fakeSession: newFakeProviderSession()}, nil
}

// GetProviderSession ...
func (ficp *FakeSanityCloudProvider) GetProviderSession(_ context.Context, _ *zap.Logger) (provider.Session, error) {
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

type fakeSnapshot struct {
	*provider.Snapshot
	tags map[string]string
}

type fakeProviderSession struct {
	provider.DefaultVolumeProvider
	volumes            map[string]*fakeVolume
	volumeAccessPoints map[string]*fakeVolumeAccessPointResponse
	snapshots          map[string]*fakeSnapshot
	pub                map[string]string
	providerName       provider.VolumeProvider
	providerType       provider.VolumeType
	tokens             map[string]int
}

func newFakeProviderSession() *fakeProviderSession {
	return &fakeProviderSession{
		volumes:            make(map[string]*fakeVolume),
		volumeAccessPoints: make(map[string]*fakeVolumeAccessPointResponse),
		snapshots:          make(map[string]*fakeSnapshot),
		pub:                make(map[string]string),
		providerName:       csiConfig.CSIProviderName,
		providerType:       csiConfig.CSIProviderVolumeType,
		tokens:             make(map[string]int),
	}
}

//##############################################################################
// Following are the fake interface methods from open source common library
// If there is any changes in the interface in the libarary then these also need
// to validate and modify accordingly
//##############################################################################

func (c *fakeProviderSession) GetSubnetForVolumeAccessPoint(_ provider.SubnetRequest) (string, error) {
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
	if len(volumeRequest.SnapshotID) > 0 {
		if _, ok := c.snapshots[volumeRequest.SnapshotID]; !ok {
			errorMsg := providerError.Message{
				Code:        "SnapshotIDNotFound",
				Description: "Snapshot ID not found",
				Type:        providerError.RetrivalFailed,
			}
			return nil, errorMsg
		}
	}
	if volumeRequest.Name == nil || len(*volumeRequest.Name) == 0 {
		return nil, errors.New("no Volume name passed")
	}
	fakeVolume := &fakeVolume{
		Volume: &provider.Volume{
			VolumeID: fmt.Sprintf("vol-uuid-test-vol-%s", uuid.New().String()[:10]),
			Name:     volumeRequest.Name,
			Region:   volumeRequest.Region,
			Capacity: volumeRequest.Capacity,
			Snapshot: provider.Snapshot{SnapshotID: volumeRequest.SnapshotID},
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

func (c *fakeProviderSession) UpdateVolume(_ provider.Volume) error {
	return nil
}

// Create the volume from snapshot with snapshot tags
func (c *fakeProviderSession) CreateVolumeFromSnapshot(_ provider.Snapshot, _ map[string]string) (*provider.Volume, error) {
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
func (c *fakeProviderSession) ListVolumes(limit int, start string, _ map[string]string) (*provider.VolumeList, error) {
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
func (c *fakeProviderSession) GetVolumeByRequestID(_ string) (*provider.Volume, error) {
	return nil, nil
}

// AuthorizeVolume allows aceess to volume  based on given authorization
func (c *fakeProviderSession) AuthorizeVolume(_ provider.VolumeAuthorization) error {
	return nil
}

// GetAttachAttachment retirves the current status of given volume attach request
func (c *fakeProviderSession) GetVolumeAttachment(_ provider.VolumeAttachmentRequest) (*provider.VolumeAttachmentResponse, error) {
	return nil, nil
}

func (c *fakeProviderSession) OrderSnapshot(_ provider.Volume) error {
	return nil
}

// Snapshot operations
// Create the snapshot on the volume
func (c *fakeProviderSession) CreateSnapshot(sourceVolumeID string, snapshotParameters provider.SnapshotParameters) (*provider.Snapshot, error) {
	snapshotID := fmt.Sprintf("crn:v1:staging:public:is:us-south-1:a/77f2bceddaeb577dcaddb4073fe82c1c::share-snapshot:vol-uuid-test-vol-%s/vol-uuid-test-vol-%s", uuid.New().String()[:10], uuid.New().String()[:10])

	fakeSnapshot := &fakeSnapshot{
		Snapshot: &provider.Snapshot{
			VolumeID:             sourceVolumeID,
			SnapshotCRN:          snapshotID,
			ReadyToUse:           false,
			SnapshotSize:         1,
			SnapshotCreationTime: time.Now(),
		},
		tags: snapshotParameters.SnapshotTags,
	}

	c.snapshots[snapshotID] = fakeSnapshot
	return fakeSnapshot.Snapshot, nil
}

// Delete the snapshot
func (c *fakeProviderSession) DeleteSnapshot(snap *provider.Snapshot) error {
	fmt.Println("DeleteSnapshot", c.snapshots)
	fmt.Println("snapshotID", snap.SnapshotID)
	for k := range c.snapshots {
		if strings.Contains(k, snap.SnapshotID) {
			fmt.Println("Found matching key:", k)
			delete(c.snapshots, k)
		}
	}
	return nil
}

// Get the snapshot
func (c *fakeProviderSession) GetSnapshot(snapshotID string, _ ...string) (*provider.Snapshot, error) {
	fmt.Println("GetSnapshot", c.snapshots)
	fmt.Println("snapshotID", snapshotID)
	for k, v := range c.snapshots {
		if strings.Contains(k, snapshotID) {
			fmt.Println("Found matching key:", k)
			return v.Snapshot, nil
		}
	}
	return nil, errors.New("error")
}

// Get the snapshot By name
func (c *fakeProviderSession) GetSnapshotByName(snapshotName string, _ ...string) (*provider.Snapshot, error) {
	if len(snapshotName) == 0 {
		return nil, errors.New("no name passed")
	}
	var snapshots []*fakeSnapshot
	for _, s := range c.snapshots {
		name, exists := s.tags["name"]
		if !exists {
			continue
		}
		if name == snapshotName {
			fmt.Println("name is same")
			snapshots = append(snapshots, s)
		}
	}
	if len(snapshots) == 0 {
		errorMsg := providerError.Message{
			Code:        "StorageFindFailedWithSnapshotName",
			Description: "Snapshot not found by name",
			Type:        providerError.RetrivalFailed,
		}
		return nil, errorMsg
	}

	return snapshots[0].Snapshot, nil
}

// Get the snapshot with volume ID
func (c *fakeProviderSession) GetSnapshotWithVolumeID(_ string, _ string) (*provider.Snapshot, error) {
	return nil, nil
}

// Snapshot list by using tags
func (c *fakeProviderSession) ListSnapshots(maxResults int, nextToken string, _ map[string]string) (*provider.SnapshotList, error) {
	var snapshots []*provider.Snapshot
	var retToken string

	if maxResults > 0 {
		r1 := rand.New(rand.NewSource(time.Now().UnixNano()))
		retToken = fmt.Sprintf("token-%d", r1.Uint64())
		c.tokens[retToken] = maxResults
		snapshots = snapshots[0:maxResults]
		fmt.Printf("%v\n", snapshots)
	}
	if len(nextToken) != 0 {
		snapshots = snapshots[c.tokens[nextToken]:]
	}
	return &provider.SnapshotList{
		Snapshots: snapshots,
		Next:      retToken,
	}, nil
}

// List all the  snapshots for a given volume
func (c *fakeProviderSession) ListAllSnapshots(_ string) ([]*provider.Snapshot, error) {
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
