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

// GetShareProfile GET to /shares/profiles/{profile-name}
func (vs *FileShareService) GetShareProfile(name string, ctxLogger *zap.Logger) (*models.ProfileDetails, error) {
	ctxLogger.Debug("Entry GetShareProfile")
	defer ctxLogger.Debug("Exit GetShareProfile")

	defer util.TimeTracker("GetShareProfile", time.Now())

	operation := &client.Operation{
		Name:        "GetShareProfile",
		Method:      "GET",
		PathPattern: shareProfileName,
	}

	var profile models.ProfileDetails
	var apiErr models.Error

	request := vs.client.NewRequest(operation)
	ctxLogger.Info("Equivalent curl command", zap.Reflect("URL", request.URL()), zap.Reflect("Operation", operation))

	req := request.PathParameter(profileName, name)
	_, err := req.JSONSuccess(&profile).JSONError(&apiErr).Invoke()
	if err != nil {
		ctxLogger.Error("Error fetching profile.", zap.Reflect("profileName: ", name), zap.Reflect("Error: ", err))
		return nil, err
	}

	ctxLogger.Info("Profile details", zap.Reflect("profile", profile))
	return &profile, nil
}
