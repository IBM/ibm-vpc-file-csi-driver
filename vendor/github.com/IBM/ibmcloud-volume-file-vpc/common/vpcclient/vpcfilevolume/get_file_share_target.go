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
	"time"

	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/client"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	"go.uber.org/zap"
)

// GetFileShareTarget GETs to /shares/{share-id}/mount_targets/{target-id}
func (vs *FileShareService) GetFileShareTarget(shareID string, targetID string, ctxLogger *zap.Logger) (*models.ShareTarget, error) {
	ctxLogger.Debug("Entry Backend GetFileShareTarget")
	defer ctxLogger.Debug("Exit Backend GetFileShareTarget")

	defer util.TimeTracker("GetFileShareTarget", time.Now())

	operation := &client.Operation{
		Name:        "GetFileShareTarget",
		Method:      "GET",
		PathPattern: shareIDPath + shareTargetIDPath,
	}

	var shareTarget models.ShareTarget
	var apiErr models.Error

	request := vs.client.NewRequest(operation)
	ctxLogger.Info("Equivalent curl command", zap.Reflect("URL", request.URL()), zap.Reflect("Operation", operation))

	req := request.PathParameter(shareIDParam, shareID).PathParameter(shareTargetIDParam, targetID)

	_, err := req.JSONSuccess(&shareTarget).JSONError(&apiErr).Invoke()
	if err != nil {
		return nil, err
	}

	return &shareTarget, nil
}

// GetFileShareTargetByName GETs /shares/{share-id}/mount_targets by target name
func (vs *FileShareService) GetFileShareTargetByName(shareID string, targetName string, ctxLogger *zap.Logger) (*models.ShareTarget, error) {
	ctxLogger.Debug("Entry Backend GetFileShareTargetByName")
	defer ctxLogger.Debug("Exit Backend GetFileShareTargetByName")

	defer util.TimeTracker("GetFileShareTargetByName", time.Now())

	// Get the file share target details for a single share target, ListFileShareTargets will return only 1 share target in list
	filters := &models.ListShareTargetFilters{ShareTargetName: targetName}
	targets, err := vs.ListFileShareTargets(targetName, filters, ctxLogger)
	if err != nil {
		return nil, err
	}

	if targets != nil {
		targetslist := targets.ShareTargets
		if len(targetslist) > 0 {
			return targetslist[0], nil
		}
	}
	return nil, err
}
