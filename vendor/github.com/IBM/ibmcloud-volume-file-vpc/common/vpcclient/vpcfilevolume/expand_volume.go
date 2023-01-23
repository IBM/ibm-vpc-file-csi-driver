/**
 * Copyright 2022 IBM Corp.
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

// ExpandVolume PATCH to /volumes
func (vs *FileShareService) ExpandVolume(shareID string, volumeTemplate *models.Share, ctxLogger *zap.Logger) (*models.Share, error) {
	ctxLogger.Debug("Entry Backend ExpandVolume")
	defer ctxLogger.Debug("Exit Backend ExpandVolume")

	defer util.TimeTracker("ExpandVolume", time.Now())

	operation := &client.Operation{
		Name:        "ExpandVolume",
		Method:      "PATCH",
		PathPattern: shareIDPath,
	}

	var share models.Share
	var apiErr models.Error

	request := vs.client.NewRequest(operation)
	req := request.PathParameter(shareIDParam, shareID)
	ctxLogger.Info("Equivalent curl command and payload details", zap.Reflect("URL", req.URL()), zap.Reflect("Payload", volumeTemplate), zap.Reflect("Operation", operation))
	_, err := req.JSONBody(volumeTemplate).JSONSuccess(&share).JSONError(&apiErr).Invoke()
	if err != nil {
		ctxLogger.Info("Exit Backend ExpandVolume due to error.", zap.Reflect("Error: ", err))
		return nil, err
	}

	return &share, nil
}
