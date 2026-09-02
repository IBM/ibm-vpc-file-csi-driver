/**
 *
 * Copyright 2026- IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache2.0
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

// Package ibmcsidriver ...
package ibmcsidriver

import (
	"fmt"

	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
)

// CapacityRoundoff is the interface the CSI driver uses to determine the
// minimum capacity (GiB) that satisfies a requested IOPS value for a given
// volume profile's bands.
//
// Implementations must be safe for concurrent use after construction.
type CapacityRoundoff interface {
	// GetMinCapacityForIops returns the minimum share capacity in GiB that
	// satisfies the requested IOPS according to the volume profile bands.
	//
	// It scans the bands (ordered from the smallest capacity band to the
	// largest) and returns the CapacityMin of the first band whose IOPSMax is
	// >= requestedIops.
	//
	// Returns an error if no band covers the requested IOPS.
	//
	// Callers are responsible for ensuring requestedIops > 0 before calling
	// this function. A zero or negative value trivially satisfies every band
	// and returns the first band's CapacityMin,
	// applyCapacityRoundoffForIops enforces this precondition.
	GetMinCapacityForIops(requestedIops int) (int, error)
}

// capacityRoundoff is the production implementation of CapacityRoundoff.
// It is a pure algorithm over a fixed volume profile band slice; it never touches the network.
type capacityRoundoff struct {
	bands []provider.VolumeProfileBand
}

// NewCapacityRoundoff constructs a CapacityRoundoff from a pre-fetched slice
// of volume profile bands (as returned by Session.GetVolumeProfileBands).
//
// This function contains no I/O — the driver can re-create it on any refresh
// cycle without making an HTTP call.
//
// The caller is responsible for passing bands in ascending IOPSMax order.
// Ordering is not enforced here because the bands are sourced from the
// armada-storage-api, which already returns them in the correct order.
// Revalidating the order on every CSI call would add unnecessary overhead.
//
// Returns an error if bands is nil or empty.
func NewCapacityRoundoff(bands []provider.VolumeProfileBand) (CapacityRoundoff, error) {
	if len(bands) == 0 {
		return nil, fmt.Errorf("ibmcsidriver: cannot create CapacityRoundoff with empty bands slice")
	}
	return &capacityRoundoff{bands: bands}, nil
}

// GetMinCapacityForIops satisfies CapacityRoundoff.
// It scans the band slice and returns the CapacityMin of the first band whose
// IOPSMax >= requestedIops.
//
// Note: IOPSMin is intentionally not checked here. The VPC API enforces the
// minimum IOPS constraint at volume creation time; this function is only
// responsible for deriving the minimum capacity that supports the requested
// IOPS upper bound.
func (r *capacityRoundoff) GetMinCapacityForIops(requestedIops int) (int, error) {
	for _, band := range r.bands {
		if int(band.IOPSMax) >= requestedIops {
			return int(band.CapacityMin), nil
		}
	}
	return 0, fmt.Errorf("ibmcsidriver: no volume profile band covers iops=%d", requestedIops)
}
