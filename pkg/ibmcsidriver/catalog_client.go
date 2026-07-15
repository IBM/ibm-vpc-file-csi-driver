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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// CatalogBand represents a single capacity-to-IOPS band from the IBM Global Catalog dp2 entry.
// Each band defines the inclusive GiB range and the inclusive IOPS range valid within that range.
type CatalogBand struct {
	CapMin  int // GiB
	CapMax  int // GiB
	IOPSMin int
	IOPSMax int
}

// CatalogClient fetches and caches dp2 capacity-IOPS bands from the IBM Global Catalog API.
// It is initialised once at driver startup; no further HTTP calls are made per PVC.
type CatalogClient struct {
	url    string
	bands  []CatalogBand
	logger *zap.Logger
}

// catalogDP2Response is the minimal subset of the JSON returned by
// GET https://globalcatalog.cloud.ibm.com/api/v1/dp2 that we need.
type catalogDP2Response struct {
	Metadata struct {
		Other struct {
			Profile struct {
				ConfigValidation []struct {
					Capacity struct {
						Min   int    `json:"min"`
						Max   int    `json:"max"`
						Units string `json:"units"`
					} `json:"capacity"`
					IOPS struct {
						Min  int    `json:"min"`
						Max  int    `json:"max"`
						Unit string `json:"unit"`
					} `json:"iops"`
				} `json:"config_validation"`
			} `json:"profile"`
		} `json:"other"`
	} `json:"metadata"`
}

// NewCatalogClient returns a CatalogClient pointed at the given URL.
// Call FetchBands immediately after construction to populate the cache.
func NewCatalogClient(url string, logger *zap.Logger) *CatalogClient {
	return &CatalogClient{url: url, logger: logger}
}

// FetchBands performs a single HTTP GET to the IBM Global Catalog API, parses the
// config_validation bands, and caches them for the lifetime of the driver pod.
// Returns an error if the endpoint is unreachable or returns an unexpected response.
func (c *CatalogClient) FetchBands() error {
	c.logger.Info("Fetching dp2 capacity-IOPS bands from IBM Global Catalog", zap.String("url", c.url))

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Get(c.url) // #nosec G107 -- URL is a driver constant, not user input
	if err != nil {
		return fmt.Errorf("catalog API request failed: %w", err)
	}
	defer resp.Body.Close() // #nosec G307

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("catalog API returned unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read catalog API response body: %w", err)
	}

	var parsed catalogDP2Response
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("failed to parse catalog API response: %w", err)
	}

	cv := parsed.Metadata.Other.Profile.ConfigValidation
	if len(cv) == 0 {
		return fmt.Errorf("catalog API returned empty config_validation array for dp2 profile")
	}

	bands := make([]CatalogBand, 0, len(cv))
	for _, entry := range cv {
		bands = append(bands, CatalogBand{
			CapMin:  entry.Capacity.Min,
			CapMax:  entry.Capacity.Max,
			IOPSMin: entry.IOPS.Min,
			IOPSMax: entry.IOPS.Max,
		})
	}
	c.bands = bands
	c.logger.Info("Successfully loaded dp2 capacity-IOPS bands from IBM Global Catalog",
		zap.Int("bandCount", len(bands)))
	return nil
}

// GetMinCapacityForIops scans the cached bands and returns the minimum capacity (GiB)
// of the first band whose IOPSMax >= requestedIops and IOPSMin <= requestedIops.
// Returns an error if no band covers the requested IOPS.
func (c *CatalogClient) GetMinCapacityForIops(requestedIops int) (int, error) {
	if len(c.bands) == 0 {
		return 0, fmt.Errorf("catalog bands not loaded; driver may not have started correctly")
	}
	for _, band := range c.bands {
		if requestedIops >= band.IOPSMin && requestedIops <= band.IOPSMax {
			return band.CapMin, nil
		}
	}
	return 0, fmt.Errorf("no dp2 band found for iops=<%d>; value may exceed the maximum supported IOPS", requestedIops)
}

// Bands returns a copy of the cached bands (used in tests and for introspection).
func (c *CatalogClient) Bands() []CatalogBand {
	out := make([]CatalogBand, len(c.bands))
	copy(out, c.bands)
	return out
}
