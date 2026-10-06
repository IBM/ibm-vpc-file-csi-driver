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

// Package provider ...
package provider

import (
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"go.uber.org/zap"
)

// GetVolumeProfileByName ...
func (vpcs *VPCSession) GetVolumeProfileByName(name string) (*provider.Profile, error) {
	vpcs.Logger.Debug("Entry of GetVolumeProfileByName method...")
	defer vpcs.Logger.Debug("Exit from GetVolumeProfileByName method...")

	vpcs.Logger.Info("Fetching Volume Profile...", zap.Reflect("VolumeProfileName", name))

	profile, err := vpcs.Apiclient.FileShareService().GetShareProfile(name, vpcs.Logger)

	if err != nil || profile == nil {
		vpcs.Logger.Warn("Error fetching Volume Profile ...", zap.Reflect("VolumeProfileName", name))
		return nil, err
	}

	// Converting lib profile to provider profile type
	respProfile := FromLibToProviderProfile(profile, vpcs.Logger)

	vpcs.Logger.Info("Volume Profile details...", zap.Reflect("respProfile", respProfile))

	return respProfile, nil
}
