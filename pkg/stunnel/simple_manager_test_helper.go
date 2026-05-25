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

package stunnel

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// NewSimpleManagerForTesting creates a SimpleManager with custom configuration for testing
// This allows tests to use temporary directories instead of hardcoded system paths
func NewSimpleManagerForTesting(servicesDir, caFile string, logger *zap.Logger) (*SimpleManager, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if servicesDir == "" {
		return nil, fmt.Errorf("servicesDir is required")
	}
	if caFile == "" {
		return nil, fmt.Errorf("caFile is required")
	}

	// Use hardcoded defaults for other settings
	initialPort := InitialPort
	portRange := PortRange
	debugLevel := DefaultDebugLevel
	debounceWindow := 2 * time.Second
	checkHost := "production.is-share.appdomain.cloud"

	sm := &SimpleManager{
		servicesDir:    servicesDir,
		initialPort:    initialPort,
		portRange:      portRange,
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		caFile:         caFile,
		checkHost:      checkHost,
		debugLevel:     debugLevel,
		logger:         logger,
		debounceWindow: debounceWindow,
	}

	// Skip recovery of existing tunnels in test mode
	logger.Info("SimpleManager initialized for testing",
		zap.String("servicesDir", servicesDir),
		zap.Int("initialPort", initialPort),
		zap.Int("portRange", portRange),
		zap.Int("debugLevel", debugLevel),
		zap.Duration("debounceWindow", debounceWindow),
		zap.String("caFile", caFile),
		zap.String("checkHost", checkHost))

	return sm, nil
}

// Made with Bob
