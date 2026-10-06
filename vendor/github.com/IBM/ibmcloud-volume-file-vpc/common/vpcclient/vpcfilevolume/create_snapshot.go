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

// Package vpcfilevolume ...
package vpcfilevolume

import (
	"time"

	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/client"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	"go.uber.org/zap"
)

// CreateSnapshot POSTs to shares/{share-id}/snapshots
func (ss *SnapshotService) CreateSnapshot(shareID string, snapshotTemplate *models.Snapshot, ctxLogger *zap.Logger) (*models.Snapshot, error) {
	ctxLogger.Debug("Entry Backend CreateSnapshot")
	defer ctxLogger.Debug("Exit Backend CreateSnapshot")

	defer util.TimeTracker("CreateSnapshot", time.Now())

	operation := &client.Operation{
		Name:        "CreateSnapshot",
		Method:      "POST",
		PathPattern: snapshotsPath,
	}

	var snapshot models.Snapshot
	var apiErr models.Error

	request := ss.client.NewRequest(operation).PathParameter(shareIDParam, shareID)
	ctxLogger.Info("Equivalent curl command and payload details", zap.Reflect("URL", request.URL()), zap.Reflect("Payload", snapshotTemplate), zap.Reflect("Operation", operation))

	_, err := request.JSONBody(snapshotTemplate).JSONSuccess(&snapshot).JSONError(&apiErr).Invoke()
	if err != nil {
		return nil, err
	}

	return &snapshot, nil
}
