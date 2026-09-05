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

// ListFileShareTargets GETs /shares/{share-id}/mount_targets
func (vs *FileShareService) ListFileShareTargets(shareID string, filters *models.ListShareTargetFilters, ctxLogger *zap.Logger) (*models.ShareTargetList, error) {
	ctxLogger.Debug("Entry Backend ListFileShareTargets")
	defer ctxLogger.Debug("Exit Backend ListFileShareTargets")

	defer util.TimeTracker("ListFileShareTargets", time.Now())

	operation := &client.Operation{
		Name:        "ListFileShareTargets",
		Method:      "GET",
		PathPattern: shareIDPath + shareTargetsPath,
	}

	var targets models.ShareTargetList
	var apiErr models.Error

	request := vs.client.NewRequest(operation)
	ctxLogger.Info("Equivalent curl command", zap.Reflect("URL", request.URL()), zap.Reflect("Operation", operation))

	req := request.PathParameter(shareIDParam, shareID)

	req = req.JSONSuccess(&targets).JSONError(&apiErr)

	if filters != nil {
		if filters.ShareTargetName != "" {
			req.AddQueryValue("name", filters.ShareTargetName)
		}
	}

	_, err := req.Invoke()
	if err != nil {
		return nil, err
	}

	return &targets, nil
}
