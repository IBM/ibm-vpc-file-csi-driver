/**
 * Copyright 2024 IBM Corp.
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

// Package provider ...
package provider

import (
	vpcprovider "github.com/IBM/ibmcloud-volume-file-vpc/file/provider"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
)

// IksVpcSession implements lib.Session for VPC IKS dual session
type IksVpcSession struct {
	vpcprovider.VPCSession                         // Holds VPC/Riaas session by default
	IksSession             *vpcprovider.VPCSession // Holds IKS session
}

var _ provider.Session = &IksVpcSession{}

const (
	// Provider storage provider
	Provider = provider.VolumeProvider("VPC-SHARE")
	// VolumeType ...
	VolumeType = provider.VolumeType("vpc-share")
)

// Close at present does nothing
func (vpcIks *IksVpcSession) Close() {
	// Do nothing for now
}

// GetProviderDisplayName returns the name of the VPC provider
func (vpcIks *IksVpcSession) GetProviderDisplayName() provider.VolumeProvider {
	return Provider
}

// ProviderName ...
func (vpcIks *IksVpcSession) ProviderName() provider.VolumeProvider {
	return Provider
}

// Type ...
func (vpcIks *IksVpcSession) Type() provider.VolumeType {
	return VolumeType
}
