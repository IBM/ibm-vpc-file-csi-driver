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

// Package stunnel provides a simple manager for stunnel service configurations
package stunnel

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

const (
	// DefaultServicesDir is where stunnel reads service configs
	DefaultServicesDir = "/etc/stunnel/services"

	// InitialPort is the starting port for tunnel allocation
	InitialPort = 10001

	// PortRange is the number of ports available
	PortRange = 20000

	// PgrepTimeout is the maximum time to wait for pgrep command
	PgrepTimeout = 5 * time.Second

	// DefaultDebugLevel is the stunnel debug verbosity (0-7, 5 recommended for production)
	DefaultDebugLevel = 5

	// NFSOverTLSPort is the standard port for NFS over TLS (VPC File Share)
	NFSOverTLSPort = 20049

	// StunnelStartupWaitTime is the time to wait for stunnel to start before first config load
	StunnelStartupWaitTime = 10 * time.Second

	// StunnelReloadWaitTime is the time to wait for stunnel to complete reload after SIGHUP
	// Based on empirical testing, stunnel takes ~4 seconds to reload configurations
	StunnelReloadWaitTime = 5 * time.Second

	// DefaultDebounceWindow is the default time window to collect multiple SIGHUPs
	DefaultDebounceWindow = 2 * time.Second

	// ProductionCheckHost is the hostname for TLS verification in production
	ProductionCheckHost = "production.is-share.appdomain.cloud"

	// StagingCheckHost is the hostname for TLS verification in staging
	StagingCheckHost = "staging.is-share.appdomain.cloud"

	// RHELCAPath is the CA bundle path for RHEL/RHCOS systems
	RHELCAPath = "/etc/host-certs/pki/ca-trust/extracted/pem/tls-ca-bundle.pem"

	// UbuntuCAPath is the CA bundle path for Ubuntu systems
	UbuntuCAPath = "/etc/host-certs/ssl/certs/ca-certificates.crt"

	// ConfigFilePermissions is the file permissions for stunnel config files (owner read/write only)
	ConfigFilePermissions = 0600
)

// SimpleManager manages stunnel service configs
type SimpleManager struct {
	mu             sync.RWMutex
	servicesDir    string
	initialPort    int
	portRange      int
	allocatedPorts map[string]int // volumeID -> port (O(1) lookup by volumeID)
	portToVolume   map[int]string // port -> volumeID (O(1) reverse lookup for port availability check)
	caFile         string         // Path to CA bundle file
	checkHost      string         // Hostname for TLS certificate verification
	stunnelStarted bool           // Tracks if stunnel has been confirmed running
	debugLevel     int            // Stunnel debug level (0-7)
	logger         *zap.Logger

	// SIGHUP debouncing fields
	debounceMu     sync.Mutex
	debounceTimer  *time.Timer
	pendingSIGHUP  bool
	debounceWindow time.Duration
}

// Config holds configuration for SimpleManager
type Config struct {
	ServicesDir    string
	InitialPort    int
	PortRange      int
	CAFile         string // Path to CA bundle file for TLS verification
	DebugLevel     int    // Stunnel debug level 0-7 (default: 5)
	Logger         *zap.Logger
	DebounceWindow time.Duration // Time window to collect multiple SIGHUPs (default: 2s)
}

// NewSimpleManager creates a new SimpleManager with hardcoded defaults
func NewSimpleManager(logger *zap.Logger) (*SimpleManager, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Hardcoded configuration defaults
	servicesDir := DefaultServicesDir
	initialPort := InitialPort
	portRange := PortRange
	debugLevel := DefaultDebugLevel
	debounceWindow := DefaultDebounceWindow

	// Auto-detect CA bundle based on OS_TYPE environment variable
	caFile, err := detectCABundle(logger)
	if err != nil || caFile == "" {
		if err != nil {
			return nil, fmt.Errorf("failed to detect CA bundle: %w", err)
		}
		return nil, fmt.Errorf("failed to detect CA bundle: empty CA bundle path")
	}

	// Determine checkHost based on CLUSTER_ENV environment variable
	checkHost, err := getClusterEnv(logger)
	if err != nil || checkHost == "" {
		if err != nil {
			return nil, fmt.Errorf("failed to determine checkHost: %w", err)
		}
		return nil, fmt.Errorf("failed to determine checkHost: empty checkHost")
	}

	// Note: servicesDir is created by Kubernetes hostPath with DirectoryOrCreate
	// No need to create it here

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

	// recoverExistingTunnels scans the services directory and rebuilds port allocation map
	if err := sm.recoverExistingTunnels(); err != nil {
		logger.Warn("Failed to rebuild port allocation map", zap.Error(err))
	}

	logger.Info("SimpleManager initialized with hardcoded defaults and SIGHUP debouncing",
		zap.String("servicesDir", servicesDir),
		zap.Int("initialPort", initialPort),
		zap.Int("portRange", portRange),
		zap.Int("debugLevel", debugLevel),
		zap.Duration("debounceWindow", debounceWindow),
		zap.String("caFile", caFile),
		zap.String("checkHost", checkHost))

	return sm, nil
}

// detectCABundle determines the system CA bundle path based on OS_TYPE environment variable
// Returns error if OS_TYPE is not set or is unknown
func detectCABundle(logger *zap.Logger) (string, error) {
	// OS_TYPE environment variable is mandatory
	osType := os.Getenv("OS_TYPE")
	if osType == "" {
		return "", fmt.Errorf("OS_TYPE environment variable is required but not set")
	}

	var caPath string
	switch osType {
	case "RHCOS", "RHEL":
		// RHEL/RHCOS path (most common in enterprise/OpenShift)
		caPath = RHELCAPath
	case "Ubuntu":
		// Ubuntu path
		caPath = UbuntuCAPath
	default:
		return "", fmt.Errorf("unknown OS_TYPE: %s (supported: RHCOS, RHEL, Ubuntu) - refusing to proceed with unknown CA configuration", osType)

	}

	// Verify CA file actually exists before returning
	if _, err := os.Stat(caPath); err != nil {
		return "", fmt.Errorf("CA bundle file not found at %s: %w - cannot establish secure TLS connections", caPath, err)
	}

	logger.Info("Detected and verified CA bundle path",
		zap.String("path", caPath),
		zap.String("osType", osType))
	return caPath, nil
}

// getClusterEnv determines the hostname for TLS certificate verification based on CLUSTER_ENV
// Defaults to production when CLUSTER_ENV is not set or is unknown
func getClusterEnv(logger *zap.Logger) (string, error) {
	clusterEnv := os.Getenv("CLUSTER_ENV")
	if clusterEnv == "" {
		clusterEnv = "production"
		logger.Warn("CLUSTER_ENV not set, defaulting to production for TLS verification",
			zap.String("clusterEnv", clusterEnv))
	}

	var checkHost string
	switch clusterEnv {
	case "production":
		checkHost = ProductionCheckHost
	case "staging":
		checkHost = StagingCheckHost
	default:
		logger.Warn("Unknown CLUSTER_ENV, defaulting to production for TLS verification",
			zap.String("clusterEnv", clusterEnv))
		checkHost = ProductionCheckHost
	}

	logger.Info("Determined checkHost for TLS verification",
		zap.String("checkHost", checkHost),
		zap.String("clusterEnv", clusterEnv))
	return checkHost, nil
}

// recoverExistingTunnels scans the services directory and rebuilds port allocation map
func (sm *SimpleManager) recoverExistingTunnels() error {
	files, err := os.ReadDir(sm.servicesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read services directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".conf" {
			continue
		}

		volumeID := strings.TrimSuffix(file.Name(), ".conf")

		// Read the config to extract port
		configPath := filepath.Join(sm.servicesDir, file.Name())
		port, err := sm.extractPortFromConfigFile(configPath)
		if err != nil {
			sm.logger.Warn("Failed to extract port from config",
				zap.String("file", file.Name()),
				zap.Error(err))
			continue
		}

		if port > 0 {
			sm.allocatedPorts[volumeID] = port
			sm.portToVolume[port] = volumeID
			sm.logger.Info("Recovered tunnel",
				zap.String("volumeID", volumeID),
				zap.Int("port", port))
		}
	}

	sm.logger.Info("Recovery complete",
		zap.Int("tunnelCount", len(sm.allocatedPorts)))

	return nil
}

// extractPortFromConfigFile extracts port from stunnel config file
// Supports various formats:
//   - accept = 127.0.0.1:PORT
//   - accept=127.0.0.1:PORT (no spaces)
//   - accept = 0.0.0.0:PORT (any IP)
func (sm *SimpleManager) extractPortFromConfigFile(configPath string) (int, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open config file %s: %w", configPath, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			sm.logger.Warn("Failed to close config file", zap.String("path", configPath), zap.Error(closeErr))
		}
	}()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Look for "accept" directive (with or without spaces around =)
		if strings.HasPrefix(line, "accept") {
			// Extract the value after "accept" and optional "="
			// Handles: "accept = value" or "accept=value"
			acceptValue := strings.TrimSpace(strings.TrimPrefix(line, "accept"))
			acceptValue = strings.TrimSpace(strings.TrimPrefix(acceptValue, "="))

			// Extract port from the address:port format
			port, err := sm.extractPortFromAddress(acceptValue, lineNum)
			if err != nil {
				return 0, fmt.Errorf("line %d: %w", lineNum, err)
			}

			// Validate port is within the manager's supported allocation range
			minPort := sm.initialPort
			maxPort := sm.initialPort + sm.portRange - 1
			if port < minPort || port > maxPort {
				return 0, fmt.Errorf("line %d: invalid port %d (supported range is %d-%d)", lineNum, port, minPort, maxPort)
			}

			// Return immediately after finding the first valid port
			return port, nil
		}
	}

	// Check for scanner errors (I/O errors during read)
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("error reading config file %s: %w", configPath, err)
	}

	return 0, fmt.Errorf("no 'accept' directive found in config file %s", configPath)
}

// extractPortFromAddress extracts port number from IPv4 or hostname address formats
// Supports: IPv4 (127.0.0.1:PORT), hostname (host:PORT)
func (sm *SimpleManager) extractPortFromAddress(address string, lineNum int) (int, error) {
	// Handle IPv4 or hostname format: 127.0.0.1:PORT or hostname:PORT
	// Find the last colon to handle cases like "host:port" correctly
	lastColon := strings.LastIndex(address, ":")
	if lastColon == -1 {
		return 0, fmt.Errorf("no port found in address (missing colon): %s", address)
	}

	portStr := strings.TrimSpace(address[lastColon+1:])
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("invalid port format in address %s: %w", address, err)
	}

	return port, nil
}

// EnsureTunnel creates or returns existing tunnel configuration
// Optimized with double-checked locking to avoid blocking when tunnel already exists
func (sm *SimpleManager) EnsureTunnel(volumeID, nfsServer, requestID string) (int, error) {
	if volumeID == "" {
		return 0, fmt.Errorf("volumeID is required")
	}

	if nfsServer == "" {
		return 0, fmt.Errorf("target IP is required")
	}

	// Fast path: Check if tunnel already exists with read lock
	// This allows multiple concurrent reads without blocking
	sm.mu.RLock()
	port, exists := sm.allocatedPorts[volumeID]
	sm.mu.RUnlock()

	if exists {
		sm.logger.Info("Tunnel already exists",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port))
		return port, nil
	}

	// Slow path: Need to create new tunnel with write lock
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double-check: another goroutine might have created it while we waited for lock (O(1) lookup)
	if port, exists := sm.allocatedPorts[volumeID]; exists {
		sm.logger.Info("Tunnel already exists (created by another goroutine)",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port))
		return port, nil
	}

	// Phase 1: Find available port (doesn't commit to maps yet)
	port, err := sm.findAvailablePort(volumeID)
	if err != nil {
		return 0, fmt.Errorf("failed to find available port: %w", err)
	}

	// Get config path for this volume
	configPath := filepath.Join(sm.servicesDir, volumeID+".conf")

	config := sm.buildTunnelConfig(volumeID, nfsServer, port)
	if err := sm.writeTunnelConfig(configPath, config); err != nil {
		return 0, err
	}

	// Phase 3: NOW commit the port allocation (file write succeeded)
	sm.allocatedPorts[volumeID] = port
	sm.portToVolume[port] = volumeID

	sm.logger.Info("Created tunnel config",
		zap.String("RequestID", requestID),
		zap.String("volumeID", volumeID),
		zap.String("nfsServer", nfsServer),
		zap.Int("port", port),
		zap.String("configPath", configPath),
		zap.Int("debugLevel", sm.debugLevel),
		zap.String("caFile", sm.caFile),
		zap.String("checkHost", sm.checkHost))

	// Check if we need to wait for stunnel to start or can send SIGHUP immediately
	// Only check if stunnel is running when we haven't confirmed it's started yet
	// This avoids running pgrep on every mount for performance
	needsWait := false
	if !sm.stunnelStarted {
		// First time or after all tunnels were removed - check if stunnel is running
		if !sm.isStunnelRunning() {
			needsWait = true
		} else {
			// Stunnel is already running
			sm.stunnelStarted = true
		}
	}

	if needsWait {
		sm.logger.Info("Stunnel not running, waiting for stunnel to start and load config",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port),
			zap.Int("allocatedPorts", len(sm.allocatedPorts)),
			zap.Duration("waitTime", StunnelStartupWaitTime))
		// Sleep to ensure stunnel container is fully started and has loaded the config
		// This prevents exit code 129 when trying to signal a non-existent process
		time.Sleep(StunnelStartupWaitTime)

		if !sm.isStunnelRunning() {
			delete(sm.allocatedPorts, volumeID)
			delete(sm.portToVolume, port)
			if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
				sm.logger.Warn("Failed to remove config file during rollback after stunnel startup failure",
					zap.String("RequestID", requestID),
					zap.String("volumeID", volumeID),
					zap.String("configPath", configPath),
					zap.Error(err))
			}

			return 0, fmt.Errorf("stunnel is still not running after waiting %v, should be retried on mount failure", StunnelStartupWaitTime)
		}

		sm.stunnelStarted = true
		sm.logger.Info("Wait complete, stunnel is running",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port))
	} else {
		// stunnel is running, schedule debounced SIGHUP
		sm.logger.Info("Stunnel running, scheduling debounced SIGHUP",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port),
			zap.Duration("debounceWindow", sm.debounceWindow))
		sm.scheduleDebouncedSIGHUP(requestID)
	}

	return port, nil
}

// scheduleDebouncedSIGHUP schedules a SIGHUP to be sent after the debounce window
// Multiple calls within the window will result in only one SIGHUP being sent
func (sm *SimpleManager) scheduleDebouncedSIGHUP(requestID string) {
	sm.debounceMu.Lock()
	defer sm.debounceMu.Unlock()

	// Mark that we have a pending SIGHUP
	sm.pendingSIGHUP = true

	// Stop existing timer and drain its channel to prevent goroutine leak
	// When Stop() returns false, the timer has already fired and sent to the channel
	// We must drain the channel to prevent the goroutine from blocking forever
	if sm.debounceTimer != nil {
		if !sm.debounceTimer.Stop() {
			// Timer already fired, drain the channel
			// Use non-blocking select to avoid deadlock if channel is already empty
			select {
			case <-sm.debounceTimer.C:
			default:
			}
		}
	}

	// Capture requestID in local variable to avoid closure capturing the outer scope variable
	// This ensures each timer callback logs the correct requestID that scheduled it,
	// preventing confusion when multiple rapid calls overwrite the outer requestID
	capturedRequestID := requestID

	// Create new timer that will send SIGHUP after debounce window
	sm.debounceTimer = time.AfterFunc(sm.debounceWindow, func() {
		sm.debounceMu.Lock()
		defer sm.debounceMu.Unlock()

		if !sm.pendingSIGHUP {
			// Already processed
			return
		}

		sm.pendingSIGHUP = false
		sm.logger.Info("Debounce window expired, sending SIGHUP",
			zap.String("RequestID", capturedRequestID),
			zap.Duration("debounceWindow", sm.debounceWindow))

		if err := sm.reloadStunnel(capturedRequestID); err != nil {
			sm.logger.Warn("Failed to send debounced SIGHUP to stunnel",
				zap.String("RequestID", capturedRequestID),
				zap.Error(err))
			// Don't fail - stunnel will pick it up via next debounce SIGHUP window
		} else {
			sm.logger.Info("Successfully sent debounced SIGHUP to stunnel",
				zap.String("RequestID", capturedRequestID))
		}
	})

	sm.logger.Info("SIGHUP debounced, will send after window",
		zap.String("RequestID", requestID),
		zap.Duration("debounceWindow", sm.debounceWindow))
}

// isStunnelRunning checks if stunnel process is currently running
func (sm *SimpleManager) isStunnelRunning() bool {
	// Try to find stunnel process with timeout
	ctx, cancel := context.WithTimeout(context.Background(), PgrepTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pgrep", "-x", "stunnel")
	err := cmd.Run()
	return err == nil
}

// buildTunnelConfig creates the stunnel service configuration content for a volume
func (sm *SimpleManager) buildTunnelConfig(volumeID, nfsServer string, port int) string {
	return fmt.Sprintf(`[%s]
client = yes
accept = 127.0.0.1:%d
connect = %s:%d
CAfile = %s
checkHost = %s
verify = 2
debug = %d
`, volumeID, port, nfsServer, NFSOverTLSPort, sm.caFile, sm.checkHost, sm.debugLevel)
}

// writeTunnelConfig writes the stunnel config directly to the final config file
func (sm *SimpleManager) writeTunnelConfig(configPath, config string) error {
	if err := os.WriteFile(configPath, []byte(config), ConfigFilePermissions); err != nil {
		return fmt.Errorf("failed to write stunnel config file: %w", err)
	}

	return nil
}

// reloadStunnel sends SIGHUP to stunnel process to reload configuration
// This requires shareProcessNamespace: true in the pod spec to work across containers
// NOTE: Only signals the stunnel process directly, NOT the wrapper script (run-stunnel.sh)
// Signaling the wrapper script causes exit code 129 and container restart
// Returns an error if multiple stunnel processes are detected (abnormal state requiring restart)

func (sm *SimpleManager) reloadStunnel(requestID string) error {
	// Find stunnel process directly with timeout
	// REQUIREMENT: Pod must have shareProcessNamespace: true to see stunnel container's processes
	ctx, cancel := context.WithTimeout(context.Background(), PgrepTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pgrep", "-x", "stunnel")
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("pgrep command timed out after %v (system may be under load)", PgrepTimeout)
		}
		return fmt.Errorf("failed to locate stunnel process; stunnel may not be running or the node server pod stunnel container may be in an unexpected state: %w", err)
	}

	pidStr := strings.TrimSpace(string(output))
	if len(pidStr) == 0 {
		return fmt.Errorf("no stunnel PIDs found in pgrep output")
	}

	// Check for multiple PIDs (pgrep returns one PID per line)
	// If there's a newline, we have multiple processes
	if strings.Contains(pidStr, "\n") {
		sm.logger.Error("Multiple stunnel processes detected - abnormal state, restart required",
			zap.String("RequestID", requestID),
			zap.String("pidStr", pidStr))
		return fmt.Errorf("multiple stunnel processes detected - this is an abnormal state, please restart the node server pod to recover")
	}

	// Parse the single PID
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return fmt.Errorf("invalid stunnel PID '%s': %w", pidStr, err)
	}

	// Send SIGHUP to the stunnel process
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		// Handle race condition: process exited between pgrep and kill
		if err == syscall.ESRCH {
			sm.logger.Warn("Stunnel process no longer exists (race condition or restart)",
				zap.String("RequestID", requestID),
				zap.Int("pid", pid),
				zap.Error(err))
			// Don't fail - stunnel will pick up configs on next reload cycle
			return nil
		}
		// Other errors (EPERM, etc.) are real problems
		return fmt.Errorf("failed to send SIGHUP to stunnel process (PID %d): %w, restart the node server pod if this continues", pid, err)
	}

	sm.logger.Info("Sent SIGHUP to stunnel process",
		zap.String("RequestID", requestID),
		zap.Int("pid", pid))
	return nil
}

// RemoveTunnel removes tunnel configuration only if no active mounts use it
func (sm *SimpleManager) RemoveTunnel(volumeID, requestID string) error {

	if volumeID == "" {
		return fmt.Errorf("volumeID is required")
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Find the port for this volume (O(1) lookup)
	tunnelPort, exists := sm.allocatedPorts[volumeID]
	if !exists {
		sm.logger.Warn("No port found for volume, config may already be removed, may not be RFS or already cleaned up",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID))
		return nil
	}

	// Check if any mounts are still using this tunnel port
	// This prevents premature deletion when multiple pods use the same volume
	if sm.isTunnelPortInUse(tunnelPort) {
		sm.logger.Info("Tunnel port still in use by active mounts, keeping config",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", tunnelPort))
		return nil
	}

	configPath := filepath.Join(sm.servicesDir, volumeID+".conf")

	// Release port from both maps
	// Note: We already verified tunnelPort exists in allocatedPorts at line 656,
	// so we know both maps are in sync. Go's delete() is safe for non-existent keys.
	delete(sm.allocatedPorts, volumeID)
	delete(sm.portToVolume, tunnelPort)

	// Check if this is the last tunnel BEFORE removing the file
	isLastTunnel := len(sm.allocatedPorts) == 0

	// If this is the last tunnel, force any pending debounced SIGHUP to fire immediately
	// This ensures stunnel reloads with just this one config, cleaning up all other listeners
	// BEFORE we remove the last config file
	// LOCK ORDERING: Must release sm.mu before acquiring sm.debounceMu to avoid deadlock
	if isLastTunnel {
		// Release sm.mu temporarily to avoid deadlock when acquiring sm.debounceMu
		sm.mu.Unlock()

		sm.debounceMu.Lock()
		if sm.pendingSIGHUP && sm.debounceTimer != nil {
			sm.logger.Info("Last tunnel being removed, forcing pending debounced SIGHUP to fire immediately",
				zap.String("RequestID", requestID),
				zap.String("volumeID", volumeID))
			sm.debounceTimer.Stop()
			sm.pendingSIGHUP = false

			// Fire SIGHUP immediately to reload with just this last config
			// This cleans up all other listeners before we remove the last config
			// reloadStunnel is called while holding debounceMu lock
			if err := sm.reloadStunnel(requestID); err != nil {
				sm.debounceMu.Unlock()
				sm.logger.Error("Failed to send SIGHUP for last tunnel removal, aborting removal",
					zap.String("requestID", requestID),
					zap.String("volumeID", volumeID),
					zap.Error(err))
				// Re-acquire sm.mu before returning
				sm.mu.Lock()
				// Rollback: Re-add port to maps since SIGHUP failed
				sm.allocatedPorts[volumeID] = tunnelPort
				sm.portToVolume[tunnelPort] = volumeID
				// Defer will unlock sm.mu
				return fmt.Errorf("failed to send SIGHUP before removing last config: %w", err)
			}

			sm.debounceMu.Unlock()
			sm.logger.Info("SIGHUP sent successfully for last tunnel, waiting for reload to complete",
				zap.String("requestID", requestID),
				zap.String("volumeID", volumeID),
				zap.Duration("waitTime", StunnelReloadWaitTime))

			// Wait for stunnel to complete the reload
			// This ensures all old listeners are cleaned up before we remove the last config
			// Re-acquire sm.mu before sleep to prevent race conditions
			sm.mu.Lock()
			time.Sleep(StunnelReloadWaitTime)
			// sm.mu remains locked after sleep, will continue to file removal
		} else {
			sm.debounceMu.Unlock()
			// Re-acquire sm.mu before continuing
			sm.mu.Lock()
		}
	}

	// Remove config file (stunnel will auto-unload the service)
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		// CRITICAL: Rollback port release to maintain consistency
		// Port was released at line 703, but file deletion failed
		// We must restore the port mapping to prevent port reuse collision
		sm.allocatedPorts[volumeID] = tunnelPort
		sm.portToVolume[tunnelPort] = volumeID

		sm.logger.Error("Failed to remove config file, rolled back port release",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", tunnelPort),
			zap.String("configPath", configPath),
			zap.Error(err))

		return fmt.Errorf("failed to remove config: %w", err)
	}

	sm.logger.Info("Removed tunnel config",
		zap.String("RequestID", requestID),
		zap.String("volumeID", volumeID),
		zap.Int("port", tunnelPort),
		zap.String("configPath", configPath),
		zap.Bool("isLastTunnel", isLastTunnel))

	// Use debounced SIGHUP for tunnel removal (same as add operations)
	// This batches multiple remove operations and prevents SIGHUP storm during scale down
	if !isLastTunnel {
		sm.logger.Info("Scheduling debounced SIGHUP for tunnel removal",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("remainingTunnels", len(sm.allocatedPorts)))

		// Unlock before calling scheduleDebouncedSIGHUP to avoid deadlock
		sm.mu.Unlock()
		sm.scheduleDebouncedSIGHUP(requestID)
		// Re-lock before return so defer can unlock properly
		sm.mu.Lock()
		return nil
	}

	sm.logger.Info("Last tunnel removed, skipping final SIGHUP",
		zap.String("RequestID", requestID))
	// Note: stunnel process keeps running (not killed/restarted)
	// stunnelStarted remains true, so next mount will use debounced SIGHUP route

	// Defer will unlock sm.mu
	return nil
}

// isTunnelPortInUse checks if any NFS mounts are using the specified tunnel port
// by reading /proc/mounts. This is fast (~2ms) and won't hang even if NFS is unresponsive.
func (sm *SimpleManager) isTunnelPortInUse(port int) bool {
	// Read /proc/mounts directly - doesn't stat the filesystem, so won't hang
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		sm.logger.Warn("Failed to read /proc/mounts, assuming port IS in use for safety",
			zap.Int("port", port),
			zap.Error(err))
		return true // Fail-safe: prevent tunnel removal if we can't verify it's safe
	}

	// Search for mounts using this port
	// Mount entries look like: 127.0.0.1:/EXPORT /mountpoint nfs4 rw,...,port=20000,... 0 0
	portStr := fmt.Sprintf("port=%d", port)
	mountCount := 0

	// Use bytes.Contains to avoid string conversion - more efficient for large files
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		// Check if this is an NFS4 mount using our tunnel port
		if strings.Contains(line, "nfs4") &&
			strings.Contains(line, "127.0.0.1:") &&
			strings.Contains(line, portStr) {
			mountCount++
			sm.logger.Debug("Found mount using tunnel port",
				zap.Int("port", port),
				zap.String("mount", line))
		}
	}

	if mountCount > 0 {
		sm.logger.Info("Tunnel port has active mounts",
			zap.Int("port", port),
			zap.Int("mountCount", mountCount))
		return true
	}

	sm.logger.Debug("Tunnel port has no active mounts",
		zap.Int("port", port))
	return false
}

// GetTunnelPort returns the port for a volume if it exists (O(1) lookup)
func (sm *SimpleManager) GetTunnelPort(volumeID string) (int, bool) {
	if volumeID == "" {
		return 0, false
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	port, exists := sm.allocatedPorts[volumeID]
	return port, exists
}

// findAvailablePort finds an available port without allocating it
// This allows for two-phase commit: find port, write file, then commit to maps
// Returns the port number if available, or error if no ports available
func (sm *SimpleManager) findAvailablePort(volumeID string) (int, error) {
	for i := 0; i < sm.portRange; i++ {
		port := sm.initialPort + i

		// Check if port is already allocated using reverse map (O(1) lookup)
		if _, portInUse := sm.portToVolume[port]; portInUse {
			continue
		}

		// Verify port is actually available on the system
		if !sm.isPortAvailable(port) {
			sm.logger.Warn("Port in use by another process, skipping",
				zap.Int("port", port),
				zap.String("volumeID", volumeID))
			continue
		}

		// Port is available - return it WITHOUT updating maps
		sm.logger.Debug("Found available port",
			zap.Int("port", port),
			zap.String("volumeID", volumeID))
		return port, nil
	}
	return 0, fmt.Errorf("no available ports in range %d-%d (all ports in use or allocated)", sm.initialPort, sm.initialPort+sm.portRange-1)
}

// isPortAvailable checks if a port is actually available on the system
// Uses DialContext (connection attempt) instead of Listen (bind) for faster check with timeout
// This prevents conflicts with other processes while being more efficient
func (sm *SimpleManager) isPortAvailable(port int) bool {
	// Fast validation of the manager's supported port range
	minPort := sm.initialPort
	maxPort := sm.initialPort + sm.portRange - 1
	if port < minPort || port > maxPort {
		return false
	}

	// Use DialContext for faster check with timeout (doesn't bind, just attempts connection)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		// Something is listening on this port - not available
		_ = conn.Close() // #nosec G104: Best effort close, error not actionable
		return false
	}

	// Check if error is "connection refused" (port available)
	// vs timeout or other errors (uncertain state)
	if opErr, ok := err.(*net.OpError); ok {
		if sysErr, ok := opErr.Err.(*os.SyscallError); ok {
			if sysErr.Err == syscall.ECONNREFUSED {
				return true // Port available - nothing listening
			}
		}
	}

	// Uncertain state (timeout, network error, etc.) - assume unavailable for safety
	sm.logger.Debug("Port availability check uncertain, assuming unavailable",
		zap.Int("port", port),
		zap.Error(err))
	return false
}

// Made with Bob
