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

// Package provider ...
package provider

import (
	"fmt"
	"time"

	"github.com/IBM/ibmcloud-volume-interface/lib/metrics"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"go.uber.org/zap"
)

// GetVolumeProfileBands retrieves the capacity-to-IOPS bands for the named profile.
func (vpcIks *IksVpcSession) GetVolumeProfileBands(profile string) ([]provider.VolumeProfileBand, error) {
	vpcIks.Logger.Debug("Entry of GetVolumeProfileBands method...", zap.String("profile", profile))
	defer vpcIks.Logger.Debug("Exit from GetVolumeProfileBands method...", zap.String("profile", profile))

	defer metrics.UpdateDurationFromStart(vpcIks.Logger, "GetVolumeProfileBands", time.Now())

	bands, err := vpcIks.IksSession.Apiclient.FileShareService().GetShareProfileBands(profile, vpcIks.Logger)
	if err != nil {
		vpcIks.Logger.Error("Failed to fetch share profile bands",
			zap.String("profile", profile),
			zap.Error(err))
		return nil, fmt.Errorf("failed to fetch share profile bands for %q: %w", profile, err)
	}

	vpcIks.Logger.Info("Successfully fetched share profile bands",
		zap.String("profile", profile),
		zap.Int("count", len(bands)))
	return bands, nil
}
