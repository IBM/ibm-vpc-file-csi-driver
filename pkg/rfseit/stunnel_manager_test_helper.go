/**
 *
 * Copyright 2026 IBM Inc. All rights reserved
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

package rfseit

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// NewStunnelManagerForTesting creates a StunnelManager with custom configuration for testing.
// This allows tests to use temporary directories instead of hardcoded system paths.
// NOTE: This file is NOT a _test.go file because it is imported by other packages' tests
// (e.g. pkg/ibmcsidriver/node_test.go), and Go does not allow cross-package access to
// symbols defined in _test.go files.
func NewStunnelManagerForTesting(servicesDir, caFile string, logger *zap.Logger) (*StunnelManager, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if servicesDir == "" {
		return nil, fmt.Errorf("servicesDir is required")
	}
	if caFile == "" {
		return nil, fmt.Errorf("caFile is required")
	}

	sm := &StunnelManager{
		servicesDir:    servicesDir,
		initialPort:    InitialPort,
		portRange:      PortRange,
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		caFile:         caFile,
		checkHost:      ProductionCheckHost,
		debugLevel:     DefaultDebugLevel,
		logger:         logger,
		debounceWindow: 2 * time.Second,
	}

	// Skip recovery of existing tunnels in test mode
	logger.Info("StunnelManager initialized for testing",
		zap.String("servicesDir", servicesDir),
		zap.Int("initialPort", sm.initialPort),
		zap.Int("portRange", sm.portRange),
		zap.Int("debugLevel", sm.debugLevel),
		zap.Duration("debounceWindow", sm.debounceWindow),
		zap.String("caFile", caFile),
		zap.String("checkHost", sm.checkHost))

	return sm, nil
}
