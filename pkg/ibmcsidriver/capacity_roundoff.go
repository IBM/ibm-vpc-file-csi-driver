/**
 *
 * Copyright 2021- IBM Inc. All rights reserved
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
	"context"
	"fmt"
	"sync"

	vpccatalog "github.com/IBM/ibmcloud-volume-file-vpc/common/catalog"
)

// CachingCatalogClient wraps any CapacityRoundoffService and caches the entire
// dp2 capacity-to-IOPS band table after the first successful fetch.
//
// Why cache the whole table, not individual IOPS lookups:
// The IBM Global Catalog returns the complete band table in a single HTTP
// response. Caching per-IOPS results would still require one HTTP call for
// each distinct IOPS value ever requested. By caching the full []Band slice
// instead, the HTTP round-trip happens exactly once per driver lifetime,
// regardless of how many different IOPS values different StorageClasses use.
// All lookups after the first fetch are pure in-memory scans.
//
// Lazy initialisation: no HTTP request is made at driver startup. The fetch
// is deferred to the first CreateVolume call that has allowCapacityRoundoffForIops
// enabled, so the driver starts up fast and has no hard dependency on catalog
// availability at boot time.
//
// Cache invalidation: a pod restart picks up any updated band table.
type CachingCatalogClient struct {
	inner vpccatalog.CapacityRoundoffService

	mu    sync.Mutex
	bands []vpccatalog.Band // nil until the first successful fetch
}

var _ vpccatalog.CapacityRoundoffService = &CachingCatalogClient{}

// NewCachingCatalogClient wraps inner with an in-process band-table cache.
// inner must not be nil.
func NewCachingCatalogClient(inner vpccatalog.CapacityRoundoffService) *CachingCatalogClient {
	return &CachingCatalogClient{inner: inner}
}

// FetchBands returns the cached band table, fetching it from the upstream
// client on the first call. The full []Band slice is stored after the first
// successful response; subsequent calls return the cached slice without any
// HTTP activity.
func (c *CachingCatalogClient) FetchBands(ctx context.Context) ([]vpccatalog.Band, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.bands) > 0 {
		return c.bands, nil
	}

	bands, err := c.inner.FetchBands(ctx)
	if err != nil {
		// Do not cache errors: a transient outage must not permanently prevent
		// future CreateVolume calls from succeeding.
		return nil, err
	}

	c.bands = bands
	return c.bands, nil
}

// GetMinimumCapacityForIOPS returns the minimum capacity, in GiB, required to
// support requestedIOPS on a dp2 share. The band table is fetched once and
// cached; subsequent calls scan the cached table in memory.
func (c *CachingCatalogClient) GetMinimumCapacityForIOPS(ctx context.Context, requestedIOPS int64) (int64, error) {
	if requestedIOPS <= 0 {
		return 0, fmt.Errorf("requested IOPS must be greater than zero: %d", requestedIOPS)
	}

	bands, err := c.FetchBands(ctx)
	if err != nil {
		return 0, err
	}

	for _, band := range bands {
		if requestedIOPS >= band.IOPS.Min && requestedIOPS <= band.IOPS.Max {
			return band.Capacity.Min, nil
		}
	}

	return 0, fmt.Errorf("no dp2 catalog band covers iops=%d", requestedIOPS)
}

// RoundUpCapacityForIOPS returns requestedCapacityGiB unchanged when it is
// already large enough for requestedIOPS, or the catalog-derived minimum
// capacity when it is too small. Uses the cached band table.
func (c *CachingCatalogClient) RoundUpCapacityForIOPS(ctx context.Context, requestedCapacityGiB, requestedIOPS int64) (int64, error) {
	minimumCapacityGiB, err := c.GetMinimumCapacityForIOPS(ctx, requestedIOPS)
	if err != nil {
		return 0, err
	}
	if requestedCapacityGiB < minimumCapacityGiB {
		return minimumCapacityGiB, nil
	}
	return requestedCapacityGiB, nil
}
