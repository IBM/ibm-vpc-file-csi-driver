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

import (
	"time"
)

// Share ...
type Share struct {
	CRN           string         `json:"crn,omitempty"`
	Href          string         `json:"href,omitempty"`
	ID            string         `json:"id,omitempty"`
	Name          string         `json:"name,omitempty"`
	Size          int64          `json:"size,omitempty"`
	Iops          int64          `json:"iops,omitempty"`
	EncryptionKey *EncryptionKey `json:"encryption_key,omitempty"`
	ResourceGroup *ResourceGroup `json:"resource_group,omitempty"`
	InitialOwner  *InitialOwner  `json:"initial_owner,omitempty"`
	Profile       *Profile       `json:"profile,omitempty"`
	CreatedAt     *time.Time     `json:"created_at,omitempty"`
	UserTags      []string       `json:"user_tags,omitempty"`
	// Status of share named - deleted, deleting, failed, pending, stable, updating, waiting, suspended
	Status            StatusType     `json:"lifecycle_state,omitempty"`
	ShareTargets      *[]ShareTarget `json:"mount_targets,omitempty"`
	Zone              *Zone          `json:"zone,omitempty"`
	AccessControlMode string         `json:"access_control_mode,omitempty"`
}

// ListShareTargerFilters ...
type ListShareTargetFilters struct {
	ShareTargetName string `json:"name,omitempty"`
}

// ListShareFilters ...
type ListShareFilters struct {
	ResourceGroupID string `json:"resource_group.id,omitempty"`
	ShareName       string `json:"name,omitempty"`
}

// ShareList ...
type ShareList struct {
	First      *HReference `json:"first,omitempty"`
	Next       *HReference `json:"next,omitempty"`
	Shares     []*Share    `json:"shares"`
	Limit      int         `json:"limit,omitempty"`
	TotalCount int         `json:"total_count,omitempty"`
}

// HReference ...
type HReference struct {
	Href string `json:"href,omitempty"`
}
