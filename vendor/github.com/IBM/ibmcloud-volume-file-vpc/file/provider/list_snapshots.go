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
	"fmt"
	"strings"
	"time"

	userError "github.com/IBM/ibmcloud-volume-file-vpc/common/messages"
	"github.com/IBM/ibmcloud-volume-file-vpc/common/vpcclient/models"
	"github.com/IBM/ibmcloud-volume-interface/lib/metrics"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"go.uber.org/zap"
)

const startSnapshoIDNotFoundMsg = "`start` parameter is invalid"

// ListSnapshots list all snapshots
func (vpcs *VPCSession) ListSnapshots(limit int, start string, filters map[string]string) (*provider.SnapshotList, error) {
	vpcs.Logger.Info("Entry ListSnapshots")
	defer vpcs.Logger.Info("Exit ListSnapshots")
	defer metrics.UpdateDurationFromStart(vpcs.Logger, "ListSnapshots", time.Now())

	if limit < 0 {
		return nil, userError.GetUserError("InvalidListSnapshotLimit", nil, limit)
	}

	if limit > maxLimit {
		vpcs.Logger.Warn(fmt.Sprintf("listSnapshots requested max entries of %v, supports values <= %v so defaulting value back to %v", limit, maxLimit, maxLimit))
		limit = maxLimit
	}

	filter := &models.LisSnapshotFilters{
		Name: filters["name"],
	}

	sourceVolumeID := filters["source_volume.id"]

	vpcs.Logger.Info("Getting snapshot list from VPC provider...", zap.Reflect("start", start), zap.Reflect("filters", filters))

	var snapshots *models.SnapshotList
	var err error
	err = retry(vpcs.Logger, func() error {
		snapshots, err = vpcs.Apiclient.SnapshotService().ListSnapshots(sourceVolumeID, limit, start, filter, vpcs.Logger)
		return err
	})

	if err != nil {
		if strings.Contains(err.Error(), startSnapshoIDNotFoundMsg) {
			return nil, userError.GetUserError("StartSnapshotIDNotFound", err, start)
		}
		return nil, userError.GetUserError("ListSnapshotsFailed", err, sourceVolumeID)
	}

	vpcs.Logger.Info("Successfully retrieved snapshot list", zap.Reflect("SnapshotList", snapshots))

	var respSnapshotList = &provider.SnapshotList{}
	if snapshots != nil {
		if snapshots.Next != nil {
			var next string
			if strings.Contains(snapshots.Next.Href, "start=") {
				next = strings.Split(strings.Split(snapshots.Next.Href, "start=")[1], "\u0026")[0]
			} else {
				vpcs.Logger.Warn("snapshots.Next.Href is not in expected format", zap.Reflect("snapshots.Next.Href", snapshots.Next.Href))
			}
			respSnapshotList.Next = next
		}

		snapshotslist := snapshots.Snapshots
		for _, snapItem := range snapshotslist {
			snapshotResponse := FromProviderToLibSnapshot(sourceVolumeID, snapItem, vpcs.Logger)
			respSnapshotList.Snapshots = append(respSnapshotList.Snapshots, snapshotResponse)
		}
	}
	return respSnapshotList, err
}
