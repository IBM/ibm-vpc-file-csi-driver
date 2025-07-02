/**
 * Copyright 2023 IBM Corp.
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

// ListSecurityGroups GETs /security_groups
func (vs *FileShareService) ListSecurityGroups(limit int, start string, filters *models.ListSecurityGroupFilters, ctxLogger *zap.Logger) (*models.SecurityGroupList, error) {
	ctxLogger.Debug("Entry Backend ListSecurityGroups")
	defer ctxLogger.Debug("Exit Backend ListSecurityGroups")

	defer util.TimeTracker("ListSecurityGroups", time.Now())

	operation := &client.Operation{
		Name:        "ListSecurityGroups",
		Method:      "GET",
		PathPattern: securityGroups,
	}

	var securityGroups models.SecurityGroupList
	var apiErr models.Error

	request := vs.client.NewRequest(operation)
	ctxLogger.Info("Equivalent curl command", zap.Reflect("URL", request.URL()), zap.Reflect("Operation", operation))

	req := request.JSONSuccess(&securityGroups).JSONError(&apiErr)

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
		if filters.VPCID != "" {
			req.AddQueryValue("vpc.id", filters.VPCID)
		}
	}

	ctxLogger.Info("Equivalent curl command", zap.Reflect("URL", req.URL()))

	_, err := req.Invoke()
	if err != nil {
		return nil, err
	}

	return &securityGroups, nil
}
