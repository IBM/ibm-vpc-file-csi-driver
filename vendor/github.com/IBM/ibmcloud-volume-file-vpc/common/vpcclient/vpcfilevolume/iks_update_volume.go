/**
 * Copyright 2024 IBM Corp.
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
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	"go.uber.org/zap"
)

// UpdateVolume POSTs to /v2/storage/updateVolume
func (vs *IKSVolumeService) UpdateVolume(pvcTemplate *provider.UpdatePVC, ctxLogger *zap.Logger) error {
	ctxLogger.Debug("Entry Backend IKSVolumeService.UpdateVolume")
	defer ctxLogger.Debug("Exit Backend IKSVolumeService.UpdateVolume")

	defer util.TimeTracker("IKSVolumeService.UpdateVolume", time.Now())

	operation := &client.Operation{
		Name:        "UpdateVolume",
		Method:      "POST",
		PathPattern: vs.pathPrefix + updateVolume,
	}
	apiErr := vs.receiverError
	request := vs.client.NewRequest(operation)
	ctxLogger.Info("Equivalent curl command", zap.Reflect("URL", request.URL()), zap.Reflect("Operation", operation), zap.Reflect("pvcTemplate", pvcTemplate))

	_, err := request.JSONBody(pvcTemplate).JSONError(apiErr).Invoke()
	if err != nil {
		ctxLogger.Error("Update volume failed with error", zap.Error(err), zap.Error(apiErr))
	}
	return err
}
