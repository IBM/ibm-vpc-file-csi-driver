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

// Package models ...
package models

import "time"

// SnapshotList ...
type SnapshotList struct {
	First      *HReference `json:"first,omitempty"`
	Next       *HReference `json:"next,omitempty"`
	Snapshots  []*Snapshot `json:"snapshots"`
	Limit      int         `json:"limit,omitempty"`
	TotalCount int         `json:"total_count,omitempty"`
}

// LisSnapshotFilters ...
type LisSnapshotFilters struct {
	Name string `json:"name,omitempty"`
}

// Snapshot ...
type Snapshot struct {
	Href             string            `json:"href,omitempty"`
	ID               string            `json:"id,omitempty"`
	Name             string            `json:"name,omitempty"`
	CRN              string            `json:"crn,omitempty"`
	FingerPrint      string            `json:"fingerprint,omitempty"`
	CreatedAt        *time.Time        `json:"created_at,omitempty"`
	CapturedAt       *time.Time        `json:"captured_at,omitempty"`
	MinimumSize      int64             `json:"minimum_size,omitempty"`
	ResourceGroup    *ResourceGroup    `json:"resource_group,omitempty"`
	Status           string            `json:"status,omitempty"`
	ResourceType     string            `json:"resource_type,omitempty"`
	LifecycleState   string            `json:"lifecycle_state,omitempty"`
	UserTags         []string          `json:"user_tags,omitempty"`
	BackupPolicyPlan *BackupPolicyPlan `json:"backup_policy_plan,omitempty"`
	Zone             *Zone             `json:"zone,omitempty"`
}

// BackupPolicyPlan ...
type BackupPolicyPlan struct {
	ID           string   `json:"id,omitempty"`
	Href         string   `json:"href,omitempty"`
	Name         string   `json:"name,omitempty"`
	Deleted      *Deleted `json:"deleted,omitempty"`
	Remote       *Remote  `json:"remote,omitempty"`
	ResourceType string   `json:"resource_type,omitempty"`
}

// Remote ...
type Remote struct {
	ID   string `json:"id,omitempty"`
	Href string `json:"href,omitempty"`
}

type Deleted struct {
	MoreInfo string `json:"more_info,omitempty"`
}
