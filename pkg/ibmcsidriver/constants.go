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

//Package ibmcsidriver ...
package ibmcsidriver

const (
	// Profile ...
	Profile = "profile"

	// IopsPerGB ...
	IopsPerGB = "iopsPerGB"

	//SizeIopsRange ...
	SizeIopsRange = "sizeIOPSRange"

	// IOPS per PVC
	IOPS = "iops"

	// SizeRangeSupported ...
	SizeRangeSupported = "sizeRange"

	// BillingType ...
	BillingType = "billingType"

	// Encrypted ..
	Encrypted = "encrypted"

	// EncryptionKey ...
	EncryptionKey = "encryptionKey"

	// ResourceGroup ...
	ResourceGroup = "resourceGroup"

	// Zone ...
	Zone = "zone"

	// Region ...
	Region = "region"

	// Tag ...
	Tag = "tags"

	// CustomProfile ...
	CustomProfile = "custom-iops"

	// DP2Profile ...
	DP2Profile = "dp2"

	// ClassVersion ...
	ClassVersion = "classVersion"

	// TrueStr ...
	TrueStr = "true"

	// FalseStr ...
	FalseStr = "false"

	// EncryptionKeyMaxLen Max length of the CRN key in Chars
	EncryptionKeyMaxLen = 256

	// ProfileNameMaxLen Max length of the profile name in Chars
	// maxLength: 63 minLength: 1 pattern: ^([a-z]|[a-z][-a-z0-9]*[a-z0-9])$
	ProfileNameMaxLen = 63

	// ResourceGroupIDMaxLen Max length of the resource group id in Chars
	// pattern: ^[0-9a-f]{32}$
	ResourceGroupIDMaxLen = 32

	// TagMaxLen Max size of tag in Chars
	// The maximum size of a tag is 128 characters.
	// The permitted characters are A-Z, 0-9, white space, underscore, hyphen,
	// period, and colon, and tags are case-insensitive.
	TagMaxLen = 128

	// ZoneNameMaxLen Max length of the Zone Name in Chars
	// maxLength: 63 minLength: 1 pattern: ^([a-z]|[a-z][-a-z0-9]*[a-z0-9])$
	ZoneNameMaxLen = 63

	// RegionMaxLen urrently same as zone
	RegionMaxLen = ZoneNameMaxLen

	// VolumeIDLabel ...
	VolumeIDLabel = "volumeId"

	// VolumeCRNLabel ...
	VolumeCRNLabel = "volumeCRN"

	// ClusterIDLabel ...
	ClusterIDLabel = "clusterID"

	// NFSServerPath ...
	NFSServerPath = "nfsServerPath"

	// AccessPointID ...
	AccessPointID = "accessPointID"

	// IOPSLabel ...
	IOPSLabel = "iops"

	// ZoneLabel ...
	ZoneLabel = "zone"

	// Generation ... just for backward compatibility
	Generation = "generation"

	// UID ...
	UID = "uid"

	// GID ...
	GID = "gid"

	// SecurityGroup
	SecurityGroup = "security_group"

	// VPC
	VPC = "vpc"

	// IsENIEnabled
	IsENIEnabled = "isENIEnabled"

	// SecurityGroupIDs
	SecurityGroupIDs = "securityGroupIDs"

	// PrimaryIPId
	PrimaryIPID = "primaryIPID"

	// PrimaryIPAddress
	PrimaryIPAddress = "primaryIPAddress"

	// SubnetID
	SubnetID = "subnetID"
)

// SupportedFS the supported FS types
var SupportedFS = []string{"nfs"}

// SupportedProfile the supported profile names
var SupportedProfile = []string{"tier-5iops", "tier-3iops", "tier-10iops", "custom-iops", "dp2"}
