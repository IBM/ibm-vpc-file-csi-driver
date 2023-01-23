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
	"time"

	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/client"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	"go.uber.org/zap"
)

// DeleteFileShareTarget DELETE to /shares/{share-id}/target/{target-id}
func (vs *FileShareService) DeleteFileShareTarget(deleteShareTargetRequest *models.ShareTarget, ctxLogger *zap.Logger) (*http.Response, error) {
	ctxLogger.Debug("Entry Backend DeleteFileShareTarget")
	defer ctxLogger.Debug("Exit Backend DeleteFileShareTarget")

	defer util.TimeTracker("DeleteFileShareTarget", time.Now())

	operation := &client.Operation{
		Name:        "DeleteFileShareTarget",
		Method:      "DELETE",
		PathPattern: shareIDPath + shareTargetIDPath,
	}

	var apiErr models.Error

	request := vs.client.NewRequest(operation)
	ctxLogger.Info("Equivalent curl command", zap.Reflect("URL", request.URL()), zap.Reflect("Operation", operation))

	resp, err := request.PathParameter(shareIDParam, deleteShareTargetRequest.ShareID).PathParameter(shareTargetIDParam, deleteShareTargetRequest.ID).JSONError(&apiErr).Invoke()
	if err != nil {
		ctxLogger.Error("Error occurred while deleting file share target", zap.Error(err))
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			// file share target is deleted. So do not want to retry
			ctxLogger.Info("Exit DeleteFileShareTarget", zap.Any("resp", resp.StatusCode), zap.Error(err), zap.Error(apiErr))
			return resp, apiErr
		}
	}
	ctxLogger.Info("DeleteFileShareTarget successfull")
	return resp, nil
}
