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

// GetFileShare POSTs to /shares/{share-id}
func (vs *FileShareService) GetFileShare(shareID string, ctxLogger *zap.Logger) (*models.Share, error) {
	ctxLogger.Debug("Entry Backend GetFileShare")
	defer ctxLogger.Debug("Exit Backend GetFileShare")

	defer util.TimeTracker("GetFileShare", time.Now())

	operation := &client.Operation{
		Name:        "GetFileShare",
		Method:      "GET",
		PathPattern: shareIDPath,
	}

	var share models.Share
	var apiErr models.Error

	request := vs.client.NewRequest(operation)
	ctxLogger.Info("Equivalent curl command", zap.Reflect("URL", request.URL()), zap.Reflect("Operation", operation))

	req := request.PathParameter(shareIDParam, shareID)
	_, err := req.JSONSuccess(&share).JSONError(&apiErr).Invoke()
	if err != nil {
		return nil, err
	}

	return &share, nil
}

// GetFileShareByName GETs /shares
func (vs *FileShareService) GetFileShareByName(shareName string, ctxLogger *zap.Logger) (*models.Share, error) {
	ctxLogger.Debug("Entry Backend GetFileShareByName")
	defer ctxLogger.Debug("Exit Backend GetFileShareByName")

	defer util.TimeTracker("GetFileShareByName", time.Now())

	// Get the file share details for a single file share, ListFileShareFilters will return only 1 file share in list
	filters := &models.ListShareFilters{ShareName: shareName}
	shares, err := vs.ListFileShares(1, "", filters, ctxLogger)
	if err != nil {
		return nil, err
	}

	if shares != nil {
		shareslist := shares.Shares
		if len(shareslist) > 0 {
			return shareslist[0], nil
		}
	}
	return nil, err
}
