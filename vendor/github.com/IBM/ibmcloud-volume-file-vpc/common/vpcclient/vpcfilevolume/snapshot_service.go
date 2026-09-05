/**
 * Copyright 2025 IBM Corp.
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

// Package vpcvolume ...
package vpcfilevolume

import (
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/client"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	"go.uber.org/zap"
)

// SnapshotManager operations
//
//go:generate counterfeiter -o fakes/snapshot.go --fake-name SnapshotManager . SnapshotManager
type SnapshotManager interface {
	// Create the snapshot on the volume
	CreateSnapshot(shareID string, snapshotTemplate *models.Snapshot, ctxLogger *zap.Logger) (*models.Snapshot, error)

	// Delete the snapshot
	DeleteSnapshot(shareID string, snapshotID string, ctxLogger *zap.Logger) error

	// Get the snapshot
	GetSnapshot(shareID string, snapshotID string, ctxLogger *zap.Logger) (*models.Snapshot, error)

	// Get the snapshot by using snapshot name
	GetSnapshotByName(shareID string, snapshotName string, ctxLogger *zap.Logger) (*models.Snapshot, error)

	// List all the  snapshots for a given volume
	ListSnapshots(shareID string, limit int, start string, filters *models.LisSnapshotFilters, ctxLogger *zap.Logger) (*models.SnapshotList, error)
}

// SnapshotService ...
type SnapshotService struct {
	client client.SessionClient
}

var _ SnapshotManager = &SnapshotService{}

// NewSnapshotManager ...
func NewSnapshotManager(client client.SessionClient) SnapshotManager {
	return &SnapshotService{
		client: client,
	}
}
