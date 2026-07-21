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

package ibmcsidriver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	vpccatalog "github.com/IBM/ibmcloud-volume-file-vpc/common/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// catalogResponseForCache is a minimal dp2 band fixture shared across all tests.
const catalogResponseForCache = `{
  "metadata": {
    "other": {
      "profile": {
        "config_validation": [
          {"capacity":{"min":10,"max":39},"iops":{"min":100,"max":1000}},
          {"capacity":{"min":40,"max":79},"iops":{"min":100,"max":2000}},
          {"capacity":{"min":80,"max":99},"iops":{"min":100,"max":4000}},
          {"capacity":{"min":100,"max":499},"iops":{"min":100,"max":6000}}
        ]
      }
    }
  }
}`

// newCountingCatalogServer returns an httptest.Server that counts every GET
// request and a CachingCatalogClient backed by that server.
func newCountingCatalogServer(t *testing.T) (*httptest.Server, *atomic.Int32, *CachingCatalogClient) {
	t.Helper()
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(catalogResponseForCache))
	}))
	t.Cleanup(srv.Close)
	inner := vpccatalog.NewCatalogClientWithEndpoint(srv.Client(), srv.URL)
	cache := NewCachingCatalogClient(inner)
	return srv, &count, cache
}

// ---------------------------------------------------------------------------
// FetchBands — whole-table cache
// ---------------------------------------------------------------------------

func TestCachingClient_FetchBandsCachesWholeTable(t *testing.T) {
	_, count, cache := newCountingCatalogServer(t)

	// First fetch — must hit upstream.
	bands1, err := cache.FetchBands(context.Background())
	require.NoError(t, err)
	require.Len(t, bands1, 4)
	assert.Equal(t, int32(1), count.Load(), "first FetchBands must cause exactly one HTTP call")

	// Second fetch — must return the cached slice without another HTTP call.
	bands2, err := cache.FetchBands(context.Background())
	require.NoError(t, err)
	require.Len(t, bands2, 4)
	assert.Equal(t, int32(1), count.Load(), "second FetchBands must use cache, not make an HTTP call")
}

func TestCachingClient_FetchBandsSortedByCapacityMin(t *testing.T) {
	_, _, cache := newCountingCatalogServer(t)

	bands, err := cache.FetchBands(context.Background())
	require.NoError(t, err)
	for i := 1; i < len(bands); i++ {
		assert.LessOrEqual(t, bands[i-1].Capacity.Min, bands[i].Capacity.Min,
			"bands must be sorted ascending by Capacity.Min")
	}
}

func TestCachingClient_FetchBandsErrorNotCached(t *testing.T) {
	// First FetchBands call fails; second succeeds. The error must not poison
	// the cache so that the success result is stored and reused thereafter.
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(catalogResponseForCache))
	}))
	defer srv.Close()

	cache := NewCachingCatalogClient(vpccatalog.NewCatalogClientWithEndpoint(srv.Client(), srv.URL))

	// Call 1 → upstream error, nothing cached.
	_, err := cache.FetchBands(context.Background())
	require.Error(t, err)

	// Call 2 → upstream success, result stored in cache.
	bands, err := cache.FetchBands(context.Background())
	require.NoError(t, err)
	require.Len(t, bands, 4)

	// Call 3 → served from cache, no HTTP call.
	bands, err = cache.FetchBands(context.Background())
	require.NoError(t, err)
	require.Len(t, bands, 4)
	assert.Equal(t, int32(2), callCount.Load(), "only two HTTP calls expected (1 error + 1 success)")
}

// ---------------------------------------------------------------------------
// GetMinimumCapacityForIOPS — single HTTP call regardless of distinct IOPS
// ---------------------------------------------------------------------------

func TestCachingClient_HitsUpstreamOnFirstCall(t *testing.T) {
	_, count, cache := newCountingCatalogServer(t)

	cap, err := cache.GetMinimumCapacityForIOPS(context.Background(), 500)
	require.NoError(t, err)
	assert.Equal(t, int64(10), cap)
	assert.Equal(t, int32(1), count.Load(), "upstream must be called once on first access")
}

func TestCachingClient_SecondCallUsesCache(t *testing.T) {
	_, count, cache := newCountingCatalogServer(t)

	for i := 0; i < 5; i++ {
		cap, err := cache.GetMinimumCapacityForIOPS(context.Background(), 500)
		require.NoError(t, err)
		assert.Equal(t, int64(10), cap)
	}
	assert.Equal(t, int32(1), count.Load(), "upstream must only be called once for repeated identical IOPS")
}

func TestCachingClient_DifferentIOPSValuesOnlyOneHTTPCall(t *testing.T) {
	_, count, cache := newCountingCatalogServer(t)

	// The whole band table is fetched once on the first call.
	// All subsequent calls for ANY IOPS value scan the cached table — no
	// additional HTTP calls, regardless of how many distinct IOPS values are used.
	iopsValues := []int64{500, 1500, 3000, 5000}
	for _, iops := range iopsValues {
		_, err := cache.GetMinimumCapacityForIOPS(context.Background(), iops)
		require.NoError(t, err)
	}
	// Call all IOPS values a second time — still only 1 HTTP call total.
	for _, iops := range iopsValues {
		_, err := cache.GetMinimumCapacityForIOPS(context.Background(), iops)
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), count.Load(),
		"the band table is fetched once; all IOPS lookups use the cached table")
}

func TestCachingClient_CorrectMinimumPerBand(t *testing.T) {
	_, _, cache := newCountingCatalogServer(t)

	testCases := []struct {
		iops    int64
		wantCap int64
	}{
		{100, 10},
		{1000, 10},
		{1001, 40},
		{2000, 40},
		{2001, 80},
		{4000, 80},
		{4001, 100},
		{6000, 100},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("iops=%d", tc.iops), func(t *testing.T) {
			cap, err := cache.GetMinimumCapacityForIOPS(context.Background(), tc.iops)
			require.NoError(t, err)
			assert.Equal(t, tc.wantCap, cap)
		})
	}
}

func TestCachingClient_GetMinimumErrorNotCached(t *testing.T) {
	// First call fails (upstream error); second call succeeds. Cache must not
	// store the error so the next call retries the HTTP fetch.
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(catalogResponseForCache))
	}))
	defer srv.Close()

	cache := NewCachingCatalogClient(vpccatalog.NewCatalogClientWithEndpoint(srv.Client(), srv.URL))

	_, err := cache.GetMinimumCapacityForIOPS(context.Background(), 500)
	require.Error(t, err)

	cap, err := cache.GetMinimumCapacityForIOPS(context.Background(), 500)
	require.NoError(t, err)
	assert.Equal(t, int64(10), cap)

	// Third call — now cached, no HTTP.
	cap, err = cache.GetMinimumCapacityForIOPS(context.Background(), 500)
	require.NoError(t, err)
	assert.Equal(t, int64(10), cap)
	assert.Equal(t, int32(2), callCount.Load(), "only two upstream calls expected")
}

func TestCachingClient_UnknownIOPSReturnsError(t *testing.T) {
	_, _, cache := newCountingCatalogServer(t)

	_, err := cache.GetMinimumCapacityForIOPS(context.Background(), 99)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no dp2 catalog band covers iops=99")
}

// ---------------------------------------------------------------------------
// RoundUpCapacityForIOPS
// ---------------------------------------------------------------------------

func TestCachingClient_RoundUpBelowMinimum(t *testing.T) {
	_, count, cache := newCountingCatalogServer(t)

	// 20 GiB at 3000 IOPS → minimum is 80 GiB.
	cap, err := cache.RoundUpCapacityForIOPS(context.Background(), 20, 3000)
	require.NoError(t, err)
	assert.Equal(t, int64(80), cap)
	assert.Equal(t, int32(1), count.Load(), "first call fetches band table")

	// Second call with different capacity but same IOPS → cache hit, no HTTP.
	cap, err = cache.RoundUpCapacityForIOPS(context.Background(), 50, 3000)
	require.NoError(t, err)
	assert.Equal(t, int64(80), cap)
	assert.Equal(t, int32(1), count.Load(), "cache must avoid second HTTP call")

	// Different IOPS value → still no HTTP call (whole table already cached).
	cap, err = cache.RoundUpCapacityForIOPS(context.Background(), 10, 5000)
	require.NoError(t, err)
	assert.Equal(t, int64(100), cap)
	assert.Equal(t, int32(1), count.Load(), "different IOPS must not cause another HTTP call")
}

func TestCachingClient_RoundUpAlreadySufficient(t *testing.T) {
	_, _, cache := newCountingCatalogServer(t)

	cap, err := cache.RoundUpCapacityForIOPS(context.Background(), 200, 3000)
	require.NoError(t, err)
	assert.Equal(t, int64(200), cap)
}

func TestCachingClient_RoundUpExactMinimum(t *testing.T) {
	_, _, cache := newCountingCatalogServer(t)

	cap, err := cache.RoundUpCapacityForIOPS(context.Background(), 80, 3000)
	require.NoError(t, err)
	assert.Equal(t, int64(80), cap)
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

func TestCachingClient_ConcurrentAccessIsSafe(t *testing.T) {
	_, count, cache := newCountingCatalogServer(t)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			cap, err := cache.GetMinimumCapacityForIOPS(context.Background(), 500)
			assert.NoError(t, err)
			assert.Equal(t, int64(10), cap)
		}()
	}
	wg.Wait()

	// All goroutines raced to warm the cache; exactly one HTTP call should occur.
	assert.Equal(t, int32(1), count.Load(),
		"concurrent cache warming must result in exactly one upstream HTTP call")
}

func TestCachingClient_ConcurrentDifferentIOPSSingleHTTPCall(t *testing.T) {
	_, count, cache := newCountingCatalogServer(t)

	iopsValues := []int64{500, 1500, 3000, 5000, 500, 1500, 3000, 5000}
	var wg sync.WaitGroup
	wg.Add(len(iopsValues))
	for _, iops := range iopsValues {
		iops := iops
		go func() {
			defer wg.Done()
			_, err := cache.GetMinimumCapacityForIOPS(context.Background(), iops)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	// Even with multiple distinct IOPS values, the band table is fetched once.
	assert.Equal(t, int32(1), count.Load(),
		"concurrent lookups with different IOPS values must still only cause one HTTP call")
}
