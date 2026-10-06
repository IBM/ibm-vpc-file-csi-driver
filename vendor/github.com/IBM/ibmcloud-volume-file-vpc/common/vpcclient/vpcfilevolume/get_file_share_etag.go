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

// GetFileShare GET to /shares/{share-id}
func (vs *FileShareService) GetFileShareEtag(shareID string, ctxLogger *zap.Logger) (*models.Share, string, error) {
	ctxLogger.Debug("Entry Backend GetFileShareEtag")
	defer ctxLogger.Debug("Exit Backend GetFileShareEtag")

	defer util.TimeTracker("GetFileShareEtag", time.Now())

	operation := &client.Operation{
		Name:        "GetFileShareEtag",
		Method:      "GET",
		PathPattern: shareIDPath,
	}

	var share models.Share
	var apiErr models.Error

	request := vs.client.NewRequest(operation)
	ctxLogger.Info("Equivalent curl command", zap.Reflect("URL", request.URL()), zap.Reflect("Operation", operation))

	req := request.PathParameter(shareIDParam, shareID)
	resp, err := req.JSONSuccess(&share).JSONError(&apiErr).Invoke()
	if err != nil {
		return nil, "", err
	}

	return &share, resp.Header.Get("etag"), nil
}
