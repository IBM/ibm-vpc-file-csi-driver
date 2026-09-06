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
	"testing"

	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testBands is the authoritative dp2 band table shared across all round-off
// test files. It exactly mirrors the IBM Global Catalog response for profile
// "dp2" as returned by armada-storage-api (verified against the live staging
// endpoint). Any change to the real catalog must be reflected here.
var testBands = []provider.VolumeProfileBand{
	{CapacityMin: 10, CapacityMax: 39, IOPSMin: 100, IOPSMax: 1000},
	{CapacityMin: 40, CapacityMax: 79, IOPSMin: 100, IOPSMax: 2000},
	{CapacityMin: 80, CapacityMax: 99, IOPSMin: 100, IOPSMax: 4000},
	{CapacityMin: 100, CapacityMax: 499, IOPSMin: 100, IOPSMax: 6000},
	{CapacityMin: 500, CapacityMax: 999, IOPSMin: 100, IOPSMax: 10000},
	{CapacityMin: 1000, CapacityMax: 1999, IOPSMin: 100, IOPSMax: 20000},
	{CapacityMin: 2000, CapacityMax: 3999, IOPSMin: 200, IOPSMax: 40000},
	{CapacityMin: 4000, CapacityMax: 7999, IOPSMin: 300, IOPSMax: 40000},
	{CapacityMin: 8000, CapacityMax: 15999, IOPSMin: 500, IOPSMax: 64000},
	{CapacityMin: 16000, CapacityMax: 32000, IOPSMin: 2000, IOPSMax: 96000},
}

// TestNewCapacityRoundoff covers construction-time validation.
func TestNewCapacityRoundoff(t *testing.T) {
	t.Run("nil slice returns error", func(t *testing.T) {
		cr, err := NewCapacityRoundoff(nil)
		assert.Nil(t, cr)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty bands slice")
	})

	t.Run("empty slice returns error", func(t *testing.T) {
		cr, err := NewCapacityRoundoff([]provider.VolumeProfileBand{})
		assert.Nil(t, cr)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty bands slice")
	})

	t.Run("valid bands returns non-nil CapacityRoundoff", func(t *testing.T) {
		cr, err := NewCapacityRoundoff(testBands)
		require.NoError(t, err)
		assert.NotNil(t, cr)
	})
}

// TestGetMinCapacityForIops covers the lookup logic.
func TestGetMinCapacityForIops(t *testing.T) {
	cr, err := NewCapacityRoundoff(testBands)
	require.NoError(t, err)

	testCases := []struct {
		name          string
		requestedIops int
		expectedCap   int
		expectError   bool
	}{
		{
			name:          "IOPS below first band maximum returns first band CapacityMin",
			requestedIops: 500,
			expectedCap:   10,
		},
		{
			name:          "IOPS exactly at a band boundary returns that band CapacityMin",
			requestedIops: 4000, // IOPSMax of the third band exactly
			expectedCap:   80,
		},
		{
			name:          "IOPS one above a band boundary falls into the next band",
			requestedIops: 4001,
			expectedCap:   100,
		},
		{
			name:          "IOPS requiring mid-table band returns correct CapacityMin",
			requestedIops: 3000,
			expectedCap:   80,
		},
		{
			// The two bands at 2000-3999 and 4000-7999 both have IOPSMax=40000;
			// the lookup returns the first matching band (CapacityMin=2000).
			name:          "IOPS exactly at the shared 40000 boundary returns first matching band CapacityMin",
			requestedIops: 40000,
			expectedCap:   2000,
		},
		{
			// 40001 exceeds both 40000-IOPS bands; next band is 8000-15999 (IOPSMax=64000).
			name:          "IOPS one above the 40000 shared boundary falls into the 8000 GiB band",
			requestedIops: 40001,
			expectedCap:   8000,
		},
		{
			name:          "IOPS at the highest band boundary returns last band CapacityMin",
			requestedIops: 96000, // IOPSMax of the last band (16000-32000 GiB) exactly
			expectedCap:   16000,
		},
		{
			name:          "IOPS one above the highest band returns error",
			requestedIops: 96001,
			expectError:   true,
		},
		{
			name:          "IOPS far above all bands returns error",
			requestedIops: 999999,
			expectError:   true,
		},
		// The following two cases document the contract for non-positive IOPS.
		// GetMinCapacityForIops itself does not validate the sign; the caller
		// (applyCapacityRoundoffForIops) is responsible for rejecting <= 0 values
		// before invoking this function. A zero or negative value would trivially
		// satisfy band.IOPSMax >= requestedIops for any band and return the first
		// band's CapacityMin — which is a silently wrong result. These tests make
		// that behaviour explicit so that any future change to add internal
		// validation here is caught immediately.
		{
			name:          "zero IOPS satisfies first band and returns first band CapacityMin (caller must prevent this)",
			requestedIops: 0,
			expectedCap:   10,
		},
		{
			name:          "negative IOPS satisfies first band and returns first band CapacityMin (caller must prevent this)",
			requestedIops: -1,
			expectedCap:   10,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := cr.GetMinCapacityForIops(tc.requestedIops)
			if tc.expectError {
				require.Error(t, err)
				assert.Equal(t, 0, got)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedCap, got)
			}
		})
	}
}
