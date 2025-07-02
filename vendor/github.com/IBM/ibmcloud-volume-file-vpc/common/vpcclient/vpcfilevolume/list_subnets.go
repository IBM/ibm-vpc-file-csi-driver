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

// ListSubnets GETs /subnets
func (vs *FileShareService) ListSubnets(limit int, start string, filters *models.ListSubnetFilters, ctxLogger *zap.Logger) (*models.SubnetList, error) {
	ctxLogger.Debug("Entry Backend ListSubnets")
	defer ctxLogger.Debug("Exit Backend ListSubnets")

	defer util.TimeTracker("ListSubnets", time.Now())

	operation := &client.Operation{
		Name:        "ListSubnets",
		Method:      "GET",
		PathPattern: subnets,
	}

	var subnets models.SubnetList
	var apiErr models.Error

	request := vs.client.NewRequest(operation)

	req := request.JSONSuccess(&subnets).JSONError(&apiErr)

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
		if filters.ZoneName != "" {
			req.AddQueryValue("zone.name", filters.ZoneName)
		}
	}

	ctxLogger.Info("Equivalent curl command", zap.Reflect("URL", req.URL()))

	_, err := req.Invoke()
	if err != nil {
		return nil, err
	}

	return &subnets, nil
}
