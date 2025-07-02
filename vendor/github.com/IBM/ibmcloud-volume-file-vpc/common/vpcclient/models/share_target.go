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

// Package models ...
package models

import (
	"time"

	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
)

// ShareTarget for riaas client
type ShareTarget struct {
	ID        string `json:"id,omitempty"`
	Href      string `json:"href,omitempty"`
	MountPath string `json:"mount_path,omitempty"`
	Name      string `json:"name,omitempty"`
	// Status of share target named - deleted, deleting, failed, pending, stable, updating, waiting, suspended
	Status string        `json:"lifecycle_state,omitempty"`
	VPC    *provider.VPC `json:"vpc,omitempty"`
	//EncryptionInTransit
	TransitEncryption       string                   `json:"transit_encryption,omitempty"`
	VirtualNetworkInterface *VirtualNetworkInterface `json:"virtual_network_interface,omitempty"`
	//Share ID this target is associated to
	ShareID   string     `json:"-"`
	Zone      *Zone      `json:"zone,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

// ShareTargetList ...
type ShareTargetList struct {
	First        *HReference    `json:"first,omitempty"`
	Next         *HReference    `json:"next,omitempty"`
	ShareTargets []*ShareTarget `json:"mount_targets,omitempty"`
	Limit        int            `json:"limit,omitempty"`
	TotalCount   int            `json:"total_count,omitempty"`
}

// VirtualNetworkInterface
type VirtualNetworkInterface struct {
	Name           string                    `json:"name,omitempty"`
	Subnet         *SubnetRef                `json:"subnet,omitempty"`
	SecurityGroups *[]provider.SecurityGroup `json:"security_groups,omitempty"`
	PrimaryIP      *provider.PrimaryIP       `json:"primary_ip,omitempty"`
	ResourceGroup  *provider.ResourceGroup   `json:"resource_group,omitempty"`
}

// NewShareTarget creates ShareTarget from VolumeAccessPointRequest
func NewShareTarget(volumeAccessPointRequest provider.VolumeAccessPointRequest) ShareTarget {
	va := ShareTarget{
		Name:    volumeAccessPointRequest.AccessPointName,
		ShareID: volumeAccessPointRequest.VolumeID,
		ID:      volumeAccessPointRequest.AccessPointID,
		VPC: &provider.VPC{
			ID: volumeAccessPointRequest.VPCID,
		},
	}

	return va
}

// ToVolumeAccessPointResponse converts ShareTargetResponse to VolumeAccessPointResponse
func (va *ShareTarget) ToVolumeAccessPointResponse() *provider.VolumeAccessPointResponse {

	varp := &provider.VolumeAccessPointResponse{
		VolumeID:      va.ShareID,
		AccessPointID: va.ID,
		Status:        va.Status,
		MountPath:     va.MountPath,
		CreatedAt:     va.CreatedAt,
	}
	return varp
}
