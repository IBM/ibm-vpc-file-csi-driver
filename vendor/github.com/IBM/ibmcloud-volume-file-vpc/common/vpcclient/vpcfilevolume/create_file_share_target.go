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

// Package instances ...
package vpcfilevolume

import (
	"time"

	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/client"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	"github.com/IBM/ibmcloud-volume-interface/lib/metrics"
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	"go.uber.org/zap"
)

// CreateFileShareTarget POSTs to /shares/{share-id}/target
// creates file share target with given share target details
func (vs *FileShareService) CreateFileShareTarget(fileShareTargetRequest *models.ShareTarget, ctxLogger *zap.Logger) (*models.ShareTarget, error) {
	methodName := "FileShareService.CreateFileShareTarget"
	defer util.TimeTracker(methodName, time.Now())
	defer metrics.UpdateDurationFromStart(ctxLogger, methodName, time.Now())

	operation := &client.Operation{
		Name:        "CreateFileShareTarget",
		Method:      "POST",
		PathPattern: shareIDPath + shareTargetsPath,
	}

	var shareTarget models.ShareTarget
	var apiErr models.Error

	request := vs.client.NewRequest(operation)

	ctxLogger.Info("Equivalent curl command and payload details", zap.Reflect("URL", request.URL()), zap.Reflect("Payload", fileShareTargetRequest), zap.Reflect("Operation", operation), zap.Reflect("PathParameters", fileShareTargetRequest.ShareID))

	req := request.PathParameter(shareIDParam, fileShareTargetRequest.ShareID)

	_, err := req.JSONBody(fileShareTargetRequest).JSONSuccess(&shareTarget).JSONError(&apiErr).Invoke()
	if err != nil {
		return nil, err
	}
	ctxLogger.Info("Successfully created the file share target")
	return &shareTarget, nil
}
