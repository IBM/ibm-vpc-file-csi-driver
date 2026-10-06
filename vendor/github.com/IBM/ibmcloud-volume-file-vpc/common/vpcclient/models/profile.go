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

// Package models ...
package models

// Profile ...
type Profile struct {
	CRN  string `json:"crn,omitempty"`
	Href string `json:"href,omitempty"`
	Name string `json:"name,omitempty"`
}

type ProfileDetails struct {
	Profile
	Capacity     CapIops `json:"capacity,omitempty"`
	Family       string  `json:"family,omitempty"`
	Iops         CapIops `json:"iops,omitempty"`
	ResourceType string  `json:"resource_type,omitempty"`
}

// CapIops
type CapIops struct {
	Default int32  `json:"default,omitempty"`
	Max     int32  `json:"max,omitempty"`
	Min     int32  `json:"min,omitempty"`
	Step    int32  `json:"step,omitempty"`
	Type    string `json:"type,omitempty"`
	Value   int32  `json:"value,omitempty"`
}

// VolumeProfileResponse is the JSON envelope for the volume profile response.
type VolumeProfileResponse struct {
	ID               string                     `json:"id"`
	ConfigValidation []VolumeProfileConfigEntry `json:"config_validation"`
}

// VolumeProfileConfigEntry is one entry in the config_validation array.
type VolumeProfileConfigEntry struct {
	Capacity VolumeProfileCapacityRange `json:"capacity"`
	IOPS     *VolumeProfileIopsRange    `json:"iops,omitempty"`
}

// VolumeProfileRange defines an inclusive min/max range.
type VolumeProfileRange struct {
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}

// VolumeProfileIopsRange is a VolumeProfileRange for IOPS values.
type VolumeProfileIopsRange VolumeProfileRange

// VolumeProfileCapacityRange is a VolumeProfileRange for capacity values in GiB.
type VolumeProfileCapacityRange VolumeProfileRange
