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
	"time"

	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/client"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	"go.uber.org/zap"
)

// UpdateVolume PATCH to /shares for updating user tags only
func (vs *FileShareService) UpdateFileShareWithEtag(shareID string, etag string, shareTemplate *models.Share, ctxLogger *zap.Logger) error {
	ctxLogger.Debug("Entry Backend UpdateVolumeWithEtag")
	defer ctxLogger.Debug("Exit Backend UpdateVolumeWithEtag")

	defer util.TimeTracker("UpdateVolumeWithEtag", time.Now())

	operation := &client.Operation{
		Name:        "UpdateFileShare",
		Method:      "PATCH",
		PathPattern: shareIDPath,
	}

	var apiErr models.Error

	request := vs.client.NewRequest(operation)
	request.SetHeader("If-Match", etag)

	req := request.PathParameter(shareIDParam, shareID)
	ctxLogger.Info("Equivalent curl command and payload details", zap.Reflect("URL", req.URL()), zap.Reflect("Payload", shareTemplate), zap.Reflect("Operation", operation))
	_, err := req.JSONBody(shareTemplate).JSONError(&apiErr).Invoke()

	if err != nil {
		return err
	}

	return nil
}
