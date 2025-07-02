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

// Package vpcfilevolume ...
package vpcfilevolume

import (
	"net/http"

	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/client"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"go.uber.org/zap"
)

// FileShareManager operations
//
//go:generate counterfeiter -o fakes/share.go --fake-name FileShareService . FileShareManager
type FileShareManager interface {
	// Create the file share with authorisation by passing required information in the share object
	CreateFileShare(volumeTemplate *models.Share, ctxLogger *zap.Logger) (*models.Share, error)

	// UpdateVolume updates the volume with authorisation by passing required information in the volume object
	UpdateVolume(pvcTemplate *provider.UpdatePVC, ctxLogger *zap.Logger) error

	// Get all file shares lists by using filter options
	ListFileShares(limit int, start string, filters *models.ListShareFilters, ctxLogger *zap.Logger) (*models.ShareList, error)

	// Get the file share by using ID
	GetFileShare(shareID string, ctxLogger *zap.Logger) (*models.Share, error)

	// Get the file share by using share name
	GetFileShareByName(shareName string, ctxLogger *zap.Logger) (*models.Share, error)

	// Get the file share etag by using ID
	GetFileShareEtag(shareID string, ctxLogger *zap.Logger) (*models.Share, string, error)

	// UpdateFileShareWithEtag updates the shares with tags by passing etag in header
	UpdateFileShareWithEtag(shareID string, etag string, shareTemplate *models.Share, ctxLogger *zap.Logger) error

	// Delete the file share
	DeleteFileShare(shareID string, ctxLogger *zap.Logger) error

	//CreateFileShareTarget creates file share target
	CreateFileShareTarget(shareTargetRequest *models.ShareTarget, ctxLogger *zap.Logger) (*models.ShareTarget, error)

	// Get file share target lists by using share ID
	ListFileShareTargets(shareID string, filters *models.ListShareTargetFilters, ctxLogger *zap.Logger) (*models.ShareTargetList, error)

	// Get the file share target by using share ID and target ID
	GetFileShareTarget(shareID string, targetID string, ctxLogger *zap.Logger) (*models.ShareTarget, error)

	// Get the file share by using share ID and target name
	GetFileShareTargetByName(targetName string, shareID string, ctxLogger *zap.Logger) (*models.ShareTarget, error)

	// DeleteFileShareTarget delete the share target by share ID and target ID/VPC ID/Subnet ID
	DeleteFileShareTarget(shareTargetDeleteRequest *models.ShareTarget, ctxLogger *zap.Logger) (*http.Response, error)

	// ExpandVolume expand the share by share ID and target
	ExpandVolume(shareID string, shareTemplate *models.Share, ctxLogger *zap.Logger) (*models.Share, error)

	// Get all subnets by using filter options
	ListSubnets(limit int, start string, filters *models.ListSubnetFilters, ctxLogger *zap.Logger) (*models.SubnetList, error)

	// Get all securityGroups by using filter options
	ListSecurityGroups(limit int, start string, filters *models.ListSecurityGroupFilters, ctxLogger *zap.Logger) (*models.SecurityGroupList, error)
}

// FileShareService ...
type FileShareService struct {
	client client.SessionClient
}

var _ FileShareManager = &FileShareService{}

// New ...
func New(client client.SessionClient) FileShareManager {
	return &FileShareService{
		client: client,
	}
}
