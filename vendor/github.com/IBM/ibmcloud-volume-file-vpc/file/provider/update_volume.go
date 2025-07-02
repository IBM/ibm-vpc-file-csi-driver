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

// Package provider ...
package provider

import (
	"strings"

	userError "github.com/IBM/ibmcloud-volume-file-vpc/common/messages"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"go.uber.org/zap"
)

// UpdateVolume PATCH to /volumes
func (vpcs *VPCSession) UpdateVolume(volumeTemplate provider.Volume) error {
	var existShare *models.Share
	var err error
	var etag string

	//Fetch existing volume Tags
	err = retryWithMinRetries(vpcs.Logger, func() error {
		// Get volume details
		existShare, etag, err = vpcs.Apiclient.FileShareService().GetFileShareEtag(volumeTemplate.VolumeID, vpcs.Logger)

		if err != nil {
			return err
		}

		if existShare != nil && existShare.Status == StatusStable {
			vpcs.Logger.Info("Volume got valid (stable) state", zap.Reflect("etag", etag))
		} else {
			return userError.GetUserError("VolumeNotInValidState", err, volumeTemplate.VolumeID)
		}

		//If tags are equal then skip the UpdateFileShare RIAAS API call
		if ifTagsEqual(existShare.UserTags, volumeTemplate.VPCVolume.Tags) {
			vpcs.Logger.Info("There is no change in user tags for volume, skipping the updateVolume for VPC IaaS... ", zap.Reflect("existShare", existShare.UserTags), zap.Reflect("volumeRequest", volumeTemplate.VPCVolume.Tags))
			return nil
		}

		//Append the existing tags with the requested input tags
		existShare.UserTags = append(existShare.UserTags, volumeTemplate.VPCVolume.Tags...)

		volume := &models.Share{
			UserTags: existShare.UserTags,
		}

		vpcs.Logger.Info("Calling VPC provider for volume UpdateVolumeWithTags...")

		err = vpcs.Apiclient.FileShareService().UpdateFileShareWithEtag(volumeTemplate.VolumeID, etag, volume, vpcs.Logger)
		return err
	})

	if err != nil {
		vpcs.Logger.Error("Failed to update volume tags from VPC provider", zap.Reflect("BackendError", err))
		return userError.GetUserError("FailedToUpdateVolume", err, volumeTemplate.VolumeID)
	}

	return err
}

// ifTagsEqual will check if there is change to existing tags
func ifTagsEqual(existingTags []string, newTags []string) bool {
	//Join slice into a string
	tags := strings.ToLower(strings.Join(existingTags, ","))
	for _, v := range newTags {
		if !strings.Contains(tags, strings.ToLower(v)) {
			//Tags are different
			return false
		}
	}
	//Tags are equal
	return true
}
