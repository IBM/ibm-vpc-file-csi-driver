/**
 * Copyright 2026 IBM Corp.
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
	"encoding/json"
	"fmt"
	"time"

	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/client"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	util "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	"go.uber.org/zap"
)

// ParseShareProfileBands decodes the GET /v2/storage/vpc/volumeProfile response
// body and returns the capacity/IOPS bands as []provider.VolumeProfileBand.
// Entries without an "iops" field are skipped.
// Returns an error if the body cannot be decoded or no IOPS bands are found.
func ParseShareProfileBands(body []byte, profile string) ([]provider.VolumeProfileBand, error) {
	var parsed models.VolumeProfileResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("volume profile: decode response: %w", err)
	}

	if len(parsed.ConfigValidation) == 0 {
		return nil, fmt.Errorf("GetVolumeProfileBands: no bands returned for profile %q", profile)
	}

	bands := make([]provider.VolumeProfileBand, 0, len(parsed.ConfigValidation))
	for _, entry := range parsed.ConfigValidation {
		if entry.IOPS == nil {
			continue
		}
		bands = append(bands, provider.VolumeProfileBand{
			CapacityMin: entry.Capacity.Min,
			CapacityMax: entry.Capacity.Max,
			IOPSMin:     entry.IOPS.Min,
			IOPSMax:     entry.IOPS.Max,
		})
	}

	if len(bands) == 0 {
		return nil, fmt.Errorf("GetVolumeProfileBands: no iops bands found for profile %q", profile)
	}
	return bands, nil
}

// GetShareProfileBands fetches the capacity/IOPS bands for the named profile.
func (vs *IKSVolumeService) GetShareProfileBands(profile string, ctxLogger *zap.Logger) ([]provider.VolumeProfileBand, error) {
	ctxLogger.Debug("Entry Backend IKSVolumeService.GetShareProfileBands")
	defer ctxLogger.Debug("Exit Backend IKSVolumeService.GetShareProfileBands")

	defer util.TimeTracker("IKSVolumeService.GetShareProfileBands", time.Now())

	operation := &client.Operation{
		Name:        "GetShareProfileBands",
		Method:      "GET",
		PathPattern: vs.pathPrefix + vpcVolumeProfile,
	}

	apiErr := vs.receiverError

	request := vs.client.NewRequest(operation)
	request.DeleteQueryValue("generation")
	request.DeleteQueryValue("version")
	// profile is passed as a query parameter, not a path segment
	request.SetQueryValue("profile", profile)
	ctxLogger.Info("Equivalent curl command", zap.Reflect("URL", request.URL()), zap.Reflect("Operation", operation))

	// Capture the raw JSON so that ParseShareProfileBands can decode it.
	var raw json.RawMessage
	_, err := request.JSONSuccess(&raw).JSONError(apiErr).Invoke()
	if err != nil {
		ctxLogger.Error("GetShareProfileBands failed", zap.String("profile", profile), zap.Error(err))
		return nil, err
	}

	bands, err := ParseShareProfileBands([]byte(raw), profile)
	if err != nil {
		ctxLogger.Error("GetShareProfileBands: parse failed", zap.String("profile", profile), zap.Error(err))
		return nil, err
	}

	ctxLogger.Info("GetShareProfileBands succeeded",
		zap.String("profile", profile),
		zap.Int("bands", len(bands)))
	return bands, nil
}
