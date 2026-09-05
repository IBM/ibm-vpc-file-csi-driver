/**
 * Copyright 2021 IBM Corp.
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
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/riaas"
	vpcconfig "github.com/IBM/ibmcloud-volume-file-vpc/file/vpcconfig"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"

	"go.uber.org/zap"
)

// VPCSession implements lib.Session
type VPCSession struct {
	provider.DefaultVolumeProvider
	VPCAccountID       string
	Config             *vpcconfig.VPCFileConfig
	ContextCredentials provider.ContextCredentials
	VolumeType         provider.VolumeType
	Provider           provider.VolumeProvider
	Apiclient          riaas.RegionalAPI
	APIVersion         string
	Logger             *zap.Logger
	APIRetry           FlexyRetry
	SessionError       error
}

const (

	// VPC storage provider
	VPC = provider.VolumeProvider("VPC-SHARE")
	// VolumeType ...
	VolumeType = provider.VolumeType("vpc-share")
)

// Close at present does nothing
func (*VPCSession) Close() {
	// Do nothing for now
}

// GetProviderDisplayName returns the name of the VPC provider
func (vpcs *VPCSession) GetProviderDisplayName() provider.VolumeProvider {
	return VPC
}

// ProviderName ...
func (vpcs *VPCSession) ProviderName() provider.VolumeProvider {
	return VPC
}

// Type ...
func (vpcs *VPCSession) Type() provider.VolumeType {
	return VolumeType
}
