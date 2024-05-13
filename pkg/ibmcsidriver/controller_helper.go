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
	"strconv"
	"strings"

	"github.com/IBM/ibm-csi-common/pkg/utils"
	"github.com/IBM/ibmcloud-volume-interface/config"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	providerError "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"go.uber.org/zap"
)

// Capacity vs IOPS range for Custom Class
type classRange struct {
	minSize int
	maxSize int
	minIops int
	maxIops int
}

// Range as per IBM volume provider Storage
var customCapacityIopsRanges = []classRange{
	{10, 39, 100, 1000},
	{40, 79, 100, 2000},
	{80, 99, 100, 4000},
	{100, 499, 100, 6000},
	{500, 999, 100, 10000},
	{1000, 1999, 100, 20000},
}

// Range as per IBM volume provider Storage for DP2 Profile - https://cloud.ibm.com/docs/vpc?topic=vpc-file-storage-profiles&interface=ui#dp2-profile
var dp2CapacityIopsRanges = []classRange{
	{10, 39, 100, 1000},
	{40, 79, 100, 2000},
	{80, 99, 100, 4000},
	{100, 499, 100, 6000},
	{500, 999, 100, 10000},
	{1000, 1999, 100, 20000},
	{2000, 3999, 200, 40000},
	{4000, 7999, 300, 40000},
	{8000, 15999, 500, 64000},
	{16000, 32000, 2000, 96000},
}

// normalize the requested capacity(in GiB) to what is supported by the driver
func getRequestedCapacity(capRange *csi.CapacityRange) (int64, error) {
	// Input is in bytes from csi
	var capBytes int64
	// Default case where nothing is set
	if capRange == nil {
		capBytes = utils.MinimumVolumeSizeInBytes
		// returns in GiB
		return capBytes, nil
	}

	rBytes := capRange.GetRequiredBytes()
	rSet := rBytes > 0
	lBytes := capRange.GetLimitBytes()
	lSet := lBytes > 0

	if lSet && rSet && lBytes < rBytes {
		return 0, fmt.Errorf("limit bytes %v is less than required bytes %v", lBytes, rBytes)
	}
	if lSet && lBytes < utils.MinimumVolumeSizeInBytes {
		return 0, fmt.Errorf("limit bytes %v is less than minimum volume size: %v", lBytes, utils.MinimumVolumeSizeInBytes)
	}

	// If Required set just set capacity to that which is Required
	if rSet {
		capBytes = rBytes
	}

	// Roundup the volume size to the next integer value
	capBytes = utils.RoundUpBytes(capBytes)

	// Limit is more than Required, but larger than Minimum. So we just set capcity to Minimum
	// Too small, default
	if capBytes < utils.MinimumVolumeSizeInBytes {
		capBytes = utils.MinimumVolumeSizeInBytes
	}

	return capBytes, nil
}

// Verify that Requested volume capabailites match with what is supported by the driver
func areVolumeCapabilitiesSupported(volCaps []*csi.VolumeCapability, driverVolumeCaps []*csi.VolumeCapability_AccessMode) bool {
	isSupport := func(cap *csi.VolumeCapability) bool {
		for _, c := range driverVolumeCaps {
			if c.GetMode() == cap.AccessMode.GetMode() {
				return true
			}
		}
		return false
	}

	allSupported := true
	for _, c := range volCaps {
		if !isSupport(c) {
			allSupported = false
		}
	}
	return allSupported
}

// getVolumeParameters this function get the parameters from storage class, this also validate
// all parameters passed in storage class or not which are mandatory.
func getVolumeParameters(logger *zap.Logger, req *csi.CreateVolumeRequest, config *config.Config) (*provider.Volume, error) {
	var encrypt = "undef"
	var err error
	var uid int
	var gid int
	volume := &provider.Volume{}
	volume.Name = &req.Name
	volume.VPCVolume.AccessControlMode = SecurityGroup //Default mode is ENI/VNI
	for key, value := range req.GetParameters() {
		switch key {
		case Profile:
			if utils.ListContainsSubstr(SupportedProfile, value) {
				volume.VPCVolume.Profile = &provider.Profile{Name: value}
			} else {
				err = fmt.Errorf("%s:<%v> unsupported profile. Supported profiles are: %v", key, value, SupportedProfile)
			}
		case Zone:
			if len(value) > ZoneNameMaxLen {
				err = fmt.Errorf("%s:<%v> exceeds %d chars", key, value, ZoneNameMaxLen)
			} else {
				volume.Az = value
			}
		case Region:
			if len(value) > RegionMaxLen {
				err = fmt.Errorf("%s:<%v> exceeds %d chars", key, value, RegionMaxLen)
			} else {
				volume.Region = value
			}
		case Tag:
			if len(value) > TagMaxLen {
				err = fmt.Errorf("%s:<%v> exceeds %d chars", key, value, TagMaxLen)
			}
			if len(value) != 0 {
				volume.VPCVolume.Tags = []string{value}
			}
		case SecurityGroupIDs:
			if len(value) != 0 {
				setSecurityGroupList(volume, value)
			}
		case PrimaryIPID:
			if len(value) != 0 {
				err = setPrimaryIPID(volume, key, value)
			}
		case PrimaryIPAddress:
			if len(value) != 0 {
				err = setPrimaryIPAddress(volume, key, value)
			}
		case SubnetID:
			if len(value) != 0 {
				volume.VPCVolume.SubnetID = value
			}
		case IsENIEnabled:
			err = checkAndSetISENIEnabled(volume, key, strings.ToLower(value))
		case IsEITEnabled:
			err = checkAndSetISEITEnabled(volume, key, strings.ToLower(value))
		case ResourceGroup:
			if len(value) > ResourceGroupIDMaxLen {
				err = fmt.Errorf("%s:<%v> exceeds %d chars", key, value, ResourceGroupIDMaxLen)
			}
			volume.VPCVolume.ResourceGroup = &provider.ResourceGroup{ID: value}

		case BillingType:
			// Its not supported by RIaaS, but this is just information for the user

		case Encrypted:
			if value != TrueStr && value != FalseStr {
				err = fmt.Errorf("'<%v>' is invalid, value of '%s' should be [true|false]", value, key)
			} else {
				encrypt = value
			}
		case EncryptionKey:
			if len(value) > EncryptionKeyMaxLen {
				err = fmt.Errorf("%s: exceeds %d bytes", key, EncryptionKeyMaxLen)
			} else {
				if len(value) != 0 {
					volume.VPCVolume.VolumeEncryptionKey = &provider.VolumeEncryptionKey{CRN: value}
				}
			}

		case ClassVersion:
			// Not needed by RIaaS, this is just info for the user
			logger.Info("Ignoring storage class parameter", zap.Any("ClassParameter", ClassVersion))

		case SizeRangeSupported:
			// Ignore... Provided in SC just as user information
			logger.Info("Ignoring storage class parameter", zap.Any("ClassParameter", SizeRangeSupported))

		case SizeIopsRange:
			// Ignore... Provided in SC just as user information
			logger.Info("Ignoring storage class parameter", zap.Any("ClassParameter", SizeIopsRange))

		case Generation:
			// Ignore... Provided in SC just for backward compatibility
			logger.Info("Ignoring storage class parameter, for backward compatibility", zap.Any("ClassParameter", Generation))

		case IOPS:
			// Default IOPS can be specified in Custom class
			if len(value) != 0 {
				iops := value
				volume.Iops = &iops
			}
		case UID:
			uid, err = strconv.Atoi(value)
			if err != nil {
				err = fmt.Errorf("failed to parse invalid %v: %v", uid, err)
			}
			if uid < 0 {
				err = fmt.Errorf("%v must be greater or equal than 0", uid)
			}
		case GID:
			gid, err = strconv.Atoi(value)
			if err != nil {
				err = fmt.Errorf("failed to parse invalid %v: %v", gid, err)
			}
			if gid < 0 {
				err = fmt.Errorf("%v must be greater or equal than 0", gid)
			}
		default:
			err = fmt.Errorf("<%s> is an invalid parameter", key)
		}
		if err != nil {
			logger.Error("getVolumeParameters", zap.NamedError("SC Parameters", err))
			return volume, err
		}
	}

	// If encripted is set to false
	if encrypt == FalseStr {
		volume.VPCVolume.VolumeEncryptionKey = nil
	}

	// Add initialOnwer if UID/GID is given as parameter.
	// Default will be set to 0 i.e root which even if not set will be defaulted to 0 by the VPC RIAAS
	if uid != 0 || gid != 0 {
		logger.Info("Adding initial owner...", zap.Any("uid", uid), zap.Any("gid", gid))
		volume.InitialOwner = &provider.InitialOwner{
			GroupID: int64(gid),
			UserID:  int64(uid),
		}
	}

	// Get the requested capacity from the request
	capacityRange := req.GetCapacityRange()
	capBytes, err := getRequestedCapacity(capacityRange)
	if err != nil {
		err = fmt.Errorf("invalid PVC capacity size: '%v'", err)
		logger.Error("getVolumeParameters", zap.NamedError("invalid parameter", err))
		return volume, err
	}
	logger.Info("Volume size in bytes", zap.Any("capacity", capBytes))

	// Convert size/capacity in GiB, as this is needed by RIaaS
	fsSize := utils.BytesToGiB(capBytes)
	// Assign the size to volume object
	volume.Capacity = &fsSize
	logger.Info("Volume size in GiB", zap.Any("capacity", fsSize))

	// volume.Capacity should be set before calling overrideParams
	err = overrideParams(logger, req, config, volume)
	if err != nil {
		return volume, err
	}

	// Check if the provided fstype is supported one
	volumeCapabilities := req.GetVolumeCapabilities()
	if volumeCapabilities == nil {
		err = fmt.Errorf("volume capabilities are empty")
		logger.Error("overrideParams", zap.NamedError("invalid parameter", err))
		return volume, err
	}

	if volume.VPCVolume.Profile != nil && volume.VPCVolume.Profile.Name != DP2Profile {
		// Specify IOPS only for custom class or DP2 class
		volume.Iops = nil
	}

	//If ENI/VNI enabled then check for scenarios where zone and subnetId is mandatory
	if volume.VPCVolume.AccessControlMode == SecurityGroup {

		//Zone and Region is mandatory if subnetID or primaryIPID/primaryIPAddress is user defined
		if (len(strings.TrimSpace(volume.Az)) == 0 || len(strings.TrimSpace(volume.Region)) == 0) && (len(volume.VPCVolume.SubnetID) != 0 || (volume.VPCVolume.PrimaryIP != nil)) {
			err = fmt.Errorf("zone and region is mandatory if subnetID or PrimaryIPID or PrimaryIPAddress is provided")
			logger.Error("getVolumeParameters", zap.NamedError("InvalidParameter", err))
			return volume, err
		}

		//subnetID is mandatory if PrimaryIPAddress is provided
		if len(volume.VPCVolume.SubnetID) == 0 && volume.VPCVolume.PrimaryIP != nil && len(volume.VPCVolume.PrimaryIP.Address) != 0 {
			err = fmt.Errorf("subnetID is mandatory if PrimaryIPAddress is provided: '%s'", volume.VPCVolume.PrimaryIP.Address)
			logger.Error("getVolumeParameters", zap.NamedError("InvalidParameter", err))
			return volume, err
		}
	}

	// For enabling EIT, check if ENI is enabled or not. If not, fail with error as to enable encryption in transit, accessControlMode must be set to security_group.
	if volume.VPCVolume.TransitEncryption == EncryptionTransitMode && volume.VPCVolume.AccessControlMode != SecurityGroup {
		err = fmt.Errorf("ENI must be enabled i.e accessControlMode must be set to security_group for creating EIT enabled fileShare. Set 'isENIEnabled' to 'true' in storage class parameters")
		logger.Error("getVolumeParameters", zap.NamedError("InvalidParameter", err))
		return volume, err
	}

	//TODO port the code from VPC BLOCK to find region if zone is given

	//If the zone is not provided in storage class parameters then we pick from the Topology
	if len(strings.TrimSpace(volume.Az)) == 0 {
		zones, err := pickTargetTopologyParams(req.GetAccessibilityRequirements())
		if err != nil {
			err = fmt.Errorf("unable to fetch zone information: '%v'", err)
			logger.Error("getVolumeParameters", zap.NamedError("InvalidParameter", err))
			return volume, err
		}
		volume.Region = zones[utils.NodeRegionLabel]
		volume.Az = zones[utils.NodeZoneLabel]
	}

	return volume, nil
}

// setSecurityGroupList
func setSecurityGroupList(volume *provider.Volume, value string) {
	securityGroupstr := strings.TrimSpace(value)
	securityGroupList := strings.Split(securityGroupstr, ",")
	var securityGroups []provider.SecurityGroup
	for _, securityGroup := range securityGroupList {
		securityGroups = append(securityGroups, provider.SecurityGroup{ID: securityGroup})
	}
	volume.VPCVolume.SecurityGroups = &securityGroups
}

// checkAndSetISENIEnabled
func checkAndSetISENIEnabled(volume *provider.Volume, key string, value string) error {
	var err error
	if value != TrueStr && value != FalseStr {
		err = fmt.Errorf("'<%v>' is invalid, value of '%s' should be [true|false]", value, key)
	} else {
		if value == TrueStr {
			volume.VPCVolume.AccessControlMode = SecurityGroup
		} else {
			volume.VPCVolume.AccessControlMode = VPC
		}
	}

	return err
}

// checkAndSetISEITEnabled
func checkAndSetISEITEnabled(volume *provider.Volume, key string, value string) error {
	var err error
	if value != TrueStr && value != FalseStr {
		err = fmt.Errorf("'<%v>' is invalid, value of '%s' should be [true|false]", value, key)
		return err
	}
	if value == TrueStr {
		volume.VPCVolume.TransitEncryption = EncryptionTransitMode
	}
	return nil
}

// setPrimaryIPID
func setPrimaryIPID(volume *provider.Volume, key string, value string) error {
	//We are failing in case PrimaryIPAddress is already set.
	if volume.VPCVolume.PrimaryIP == nil {
		volume.VPCVolume.PrimaryIP = &provider.PrimaryIP{PrimaryIPID: provider.PrimaryIPID{ID: value}}
		return nil
	}

	return fmt.Errorf("invalid option either provide primaryIPID or primaryIPAddress: '%s:<%v>'", key, value)
}

// setPrimaryIPAddress
func setPrimaryIPAddress(volume *provider.Volume, key string, value string) error {
	//We are failing in case PrimaryIPID is already set.
	if volume.VPCVolume.PrimaryIP == nil {
		volume.VPCVolume.PrimaryIP = &provider.PrimaryIP{PrimaryIPAddress: provider.PrimaryIPAddress{Address: value}}
		return nil
	}

	return fmt.Errorf("invalid option either provide primaryIPID or primaryIPAddress: '%s:<%v>'", key, value)
}

// Validate size and iops for custom class
func isValidCapacityIOPS(size int, iops int, profile string) (bool, error) {
	var ind = -1
	var capacityIopsRanges []classRange

	if profile == DP2Profile {
		capacityIopsRanges = dp2CapacityIopsRanges
	} else {
		return false, fmt.Errorf("invalid profile: <%s>", profile)
	}

	for i, entry := range capacityIopsRanges {
		if size >= entry.minSize && size <= entry.maxSize {
			ind = i
			break
		}
	}

	if ind < 0 {
		return false, fmt.Errorf("invalid PVC size for class: <%v>. Should be in range [%d - %d]GiB",
			size, utils.MinimumVolumeDiskSizeInGb, utils.MaximumVolumeDiskSizeInGb)
	}

	if iops < capacityIopsRanges[ind].minIops || iops > capacityIopsRanges[ind].maxIops {
		return false, fmt.Errorf("invalid IOPS: <%v> for capacity: <%vGiB>. Should be in range [%d - %d]",
			iops, size, capacityIopsRanges[ind].minIops, capacityIopsRanges[ind].maxIops)
	}
	return true, nil
}

func overrideParams(logger *zap.Logger, req *csi.CreateVolumeRequest, config *config.Config, volume *provider.Volume) error {
	var encrypt = "undef"
	var err error
	if volume == nil {
		return fmt.Errorf("invalid volume parameter")
	}

	for key, value := range req.GetSecrets() {
		switch key {
		case ResourceGroup:
			if len(value) > ResourceGroupIDMaxLen {
				err = fmt.Errorf("%s:<%v> exceeds %d bytes ", key, value, ResourceGroupIDMaxLen)
			} else {
				logger.Info("override", zap.Any(ResourceGroup, value))
				volume.VPCVolume.ResourceGroup = &provider.ResourceGroup{ID: value}
			}
		case Encrypted:
			if value != TrueStr && value != FalseStr {
				err = fmt.Errorf("<%v> is invalid, value for '%s' should be [true|false]", value, key)
			} else {
				logger.Info("override", zap.Any(Encrypted, value))
				encrypt = value
			}
		case EncryptionKey:
			if len(value) > EncryptionKeyMaxLen {
				err = fmt.Errorf("%s exceeds %d bytes", key, EncryptionKeyMaxLen)
			} else {
				if len(value) != 0 {
					logger.Info("override", zap.String("parameter", EncryptionKey))
					volume.VPCVolume.VolumeEncryptionKey = &provider.VolumeEncryptionKey{CRN: value}
				}
			}
		case Tag:
			if len(value) > TagMaxLen {
				err = fmt.Errorf("%s:<%v> exceeds %d chars", key, value, TagMaxLen)
			} else {
				if len(value) != 0 {
					logger.Info("append", zap.Any(Tag, value))
					volume.VPCVolume.Tags = append(volume.VPCVolume.Tags, value)
				}
			}
		case Zone:
			if len(value) > ZoneNameMaxLen {
				err = fmt.Errorf("%s:<%v> exceeds %d chars", key, value, ZoneNameMaxLen)
			} else {
				logger.Info("override", zap.Any(Zone, value))
				volume.Az = value
			}
		case Region:
			if len(value) > RegionMaxLen {
				err = fmt.Errorf("%s:<%v> exceeds %d chars", key, value, RegionMaxLen)
			} else {
				volume.Region = value
			}
		case IOPS:
			// Override IOPS only for custom or dp2
			if volume.Capacity != nil && volume.VPCVolume.Profile != nil && volume.VPCVolume.Profile.Name == DP2Profile {
				var iops int
				var check bool
				iops, err = strconv.Atoi(value)
				if err != nil {
					err = fmt.Errorf("%v:<%v> invalid value", key, value)
				} else {
					if check, err = isValidCapacityIOPS(*(volume.Capacity), iops, volume.VPCVolume.Profile.Name); check {
						iopsStr := value
						logger.Info("override", zap.Any(IOPS, value))
						volume.Iops = &iopsStr
					}
				}
			}
		case SecurityGroupIDs:
			if len(value) != 0 {
				setSecurityGroupList(volume, value)
			}
		case PrimaryIPID:
			if len(value) != 0 {
				err = setPrimaryIPID(volume, key, value)
			}
		case PrimaryIPAddress:
			if len(value) != 0 {
				err = setPrimaryIPAddress(volume, key, value)
			}
		case SubnetID:
			if len(value) != 0 {
				volume.VPCVolume.SubnetID = value
			}
		case IsENIEnabled:
			err = checkAndSetISENIEnabled(volume, key, strings.ToLower(value))
		case IsEITEnabled:
			err = checkAndSetISEITEnabled(volume, key, strings.ToLower(value))
		default:
			err = fmt.Errorf("<%s> is an invalid parameter", key)
		}
		if err != nil {
			logger.Error("overrideParams", zap.NamedError("Secret Parameters", err))
			return err
		}
	}
	// Assign ResourceGroupID from config
	if volume.VPCVolume.ResourceGroup == nil || len(volume.VPCVolume.ResourceGroup.ID) < 1 {
		volume.VPCVolume.ResourceGroup = &provider.ResourceGroup{ID: config.VPC.G2ResourceGroupID}
	}
	if encrypt == FalseStr {
		volume.VPCVolume.VolumeEncryptionKey = nil
	}
	return nil
}

// checkIfVolumeExists ...
func checkIfVolumeExists(session provider.Session, vol provider.Volume, ctxLogger *zap.Logger) (*provider.Volume, error) {
	// Check if Requested Volume exists
	// Cases to check - If Volume is Not Found,  Multiple Disks with same name, or Size Don't match
	// Todo: convert to switch statement.
	var err error
	var existingVol *provider.Volume

	if vol.Name != nil && *vol.Name != "" {
		existingVol, err = session.GetVolumeByName(*vol.Name)
	} else if vol.VolumeID != "" {
		existingVol, err = session.GetVolume(vol.VolumeID)
	} else {
		return nil, fmt.Errorf("both volume name and ID are nil")
	}

	if err != nil {
		ctxLogger.Error("checkIfVolumeExists", zap.NamedError("Error", err))
		errorType := providerError.GetErrorType(err)
		switch errorType {
		case providerError.EntityNotFound:
			return nil, nil
		case providerError.RetrivalFailed:
			return nil, nil
		default:
			return nil, err
		}
	}
	// Update the region as its not getting updated in the common library because
	// RIaaS does not provide Region details
	if existingVol != nil {
		existingVol.Region = vol.Region
	}

	return existingVol, err
}

// createCSIVolumeResponse ...
func createCSIVolumeResponse(vol provider.Volume, volAccessPointResponse provider.VolumeAccessPointResponse, capBytes int64, zones []string, clusterID string) *csi.CreateVolumeResponse {
	labels := map[string]string{}

	// Update labels for PV objects
	labels[VolumeIDLabel] = vol.VolumeID + ":" + volAccessPointResponse.AccessPointID
	labels[VolumeCRNLabel] = vol.CRN
	labels[ClusterIDLabel] = clusterID
	labels[Tag] = strings.Join(vol.Tags, ",")
	if vol.Iops != nil && len(*vol.Iops) > 0 {
		labels[IOPSLabel] = *vol.Iops
	}

	labels[utils.NodeRegionLabel] = vol.Region

	topology := &csi.Topology{
		Segments: map[string]string{
			utils.NodeRegionLabel: labels[utils.NodeRegionLabel],
		},
	}
	//As cross zone mounting is supported for ENI/VNI lets not populate this for securityGroup Mode.
	if vol.AccessControlMode == VPC {
		labels[utils.NodeZoneLabel] = vol.Az
		topology.Segments[utils.NodeZoneLabel] = labels[utils.NodeZoneLabel]
	}

	labels[NFSServerPath] = volAccessPointResponse.MountPath

	// Update label in case EIT is enabled
	if vol.TransitEncryption == EncryptionTransitMode {
		labels[IsEITEnabled] = TrueStr
	}

	// Create csi volume response
	//Volume ID is in format volumeID:volumeAccessPointID, to assist the deletion of access point in delete volume
	volResp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			CapacityBytes:      capBytes,
			VolumeId:           vol.VolumeID + ":" + volAccessPointResponse.AccessPointID,
			VolumeContext:      labels,
			AccessibleTopology: []*csi.Topology{topology},
		},
	}

	return volResp
}

func pickTargetTopologyParams(top *csi.TopologyRequirement) (map[string]string, error) {
	prefTopologyParams, err := getPrefedTopologyParams(top.GetPreferred())
	if err != nil {
		return nil, fmt.Errorf("could not get zones from preferred topology: %v", err)
	}

	return prefTopologyParams, nil
}

func getPrefedTopologyParams(topList []*csi.Topology) (map[string]string, error) {
	for _, top := range topList {
		segment := top.GetSegments()
		if segment != nil {
			return segment, nil
		}
	}
	return nil, fmt.Errorf("preferred topologies specified but no segments")
}
