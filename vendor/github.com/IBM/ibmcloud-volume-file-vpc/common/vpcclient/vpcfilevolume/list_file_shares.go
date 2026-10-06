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
	"strconv"
	"time"

	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/client"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	"go.uber.org/zap"
)

// ListFileShares GETs /shares
func (vs *FileShareService) ListFileShares(limit int, start string, filters *models.ListShareFilters, ctxLogger *zap.Logger) (*models.ShareList, error) {
	ctxLogger.Debug("Entry Backend ListFileShares")
	defer ctxLogger.Debug("Exit Backend ListFileShares")

	defer util.TimeTracker("ListFileShares", time.Now())

	operation := &client.Operation{
		Name:        "ListFileShares",
		Method:      "GET",
		PathPattern: sharesPath,
	}

	var shares models.ShareList
	var apiErr models.Error

	request := vs.client.NewRequest(operation)
	ctxLogger.Info("Equivalent curl command", zap.Reflect("URL", request.URL()), zap.Reflect("Operation", operation))

	req := request.JSONSuccess(&shares).JSONError(&apiErr)

	if limit > 0 {
		req.AddQueryValue("limit", strconv.Itoa(limit))
	}

	if start != "" {
		req.AddQueryValue("start", start)
	}

	if filters != nil {
		if filters.ResourceGroupID != "" {
			req.AddQueryValue("resource_group.id", filters.ResourceGroupID)
		}
		if filters.ShareName != "" {
			req.AddQueryValue("name", filters.ShareName)
		}
	}

	_, err := req.Invoke()
	if err != nil {
		return nil, err
	}

	return &shares, nil
}
