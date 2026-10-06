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

// Package provider ...
package provider

import (
	userError "github.com/IBM/ibmcloud-volume-file-vpc/common/messages"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"go.uber.org/zap"
)

// GetSnapshot get snapshot
func (vpcs *VPCSession) GetSnapshot(snapshotID string, sourceVolumeID ...string) (*provider.Snapshot, error) {
	vpcs.Logger.Info("Entry GetSnapshot", zap.Reflect("SnapshotID", snapshotID))
	defer vpcs.Logger.Info("Exit GetSnapshot", zap.Reflect("SnapshotID", snapshotID))

	vpcs.Logger.Info("Getting snapshot details from VPC provider...", zap.Reflect("SnapshotID", snapshotID))

	if len(sourceVolumeID) == 0 {
		return nil, userError.GetUserError("ErrorRequiredFieldMissing", nil, "sourceVolumeID")
	}

	var snapshot *models.Snapshot
	var err error
	err = retry(vpcs.Logger, func() error {
		snapshot, err = vpcs.Apiclient.SnapshotService().GetSnapshot(sourceVolumeID[0], snapshotID, vpcs.Logger)
		return err
	})

	if err != nil {
		return nil, userError.GetUserError("FailedToFindSnapshot", err, snapshotID, sourceVolumeID)
	}

	vpcs.Logger.Info("Successfully retrieved snpashot details from VPC backend", zap.Reflect("snapshotDetails", snapshot))
	snapshotResponse := FromProviderToLibSnapshot(sourceVolumeID[0], snapshot, vpcs.Logger)
	vpcs.Logger.Info("SnapshotResponse", zap.Reflect("snapshotResponse", snapshotResponse))
	return snapshotResponse, err
}

// GetSnapshotByName ...
func (vpcs *VPCSession) GetSnapshotByName(name string, sourceVolumeID ...string) (respSnap *provider.Snapshot, err error) {
	vpcs.Logger.Debug("Entry of GetSnapshotByName method...")
	defer vpcs.Logger.Debug("Exit from GetSnapshotByName method...")

	if len(sourceVolumeID) == 0 {
		return nil, userError.GetUserError("ErrorRequiredFieldMissing", nil, "sourceVolumeID")
	}

	vpcs.Logger.Info("Basic validation for snapshot Name...", zap.Reflect("SnapshotName", name))
	if len(name) <= 0 {
		err = userError.GetUserError("ErrorRequiredFieldMissing", nil, "SnapshotName")
		return
	}

	vpcs.Logger.Info("Getting snapshot details from VPC provider...", zap.Reflect("SnapshotName", name))

	var snapshot *models.Snapshot
	err = retry(vpcs.Logger, func() error {
		snapshot, err = vpcs.Apiclient.SnapshotService().GetSnapshotByName(sourceVolumeID[0], name, vpcs.Logger)
		return err
	})

	if err != nil {
		return nil, userError.GetUserError("StorageFindFailedWithSnapshotName", err, name, sourceVolumeID)
	}

	vpcs.Logger.Info("Successfully retrieved snpashot details", zap.Reflect("snapshotDetails", snapshot))
	snapshotResponse := FromProviderToLibSnapshot(sourceVolumeID[0], snapshot, vpcs.Logger)
	vpcs.Logger.Info("SnapshotResponse", zap.Reflect("snapshotResponse", snapshotResponse))
	return snapshotResponse, err
}
