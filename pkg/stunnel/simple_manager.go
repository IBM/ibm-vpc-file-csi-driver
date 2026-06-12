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

	// Auto-detect CA bundle based on OS_TYPE environment variable
	caFile, err := detectCABundle(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to detect CA bundle: %w, please open support ticket", err)
	}
	if caFile == "" {
		return nil, fmt.Errorf("failed to detect CA bundle: empty CA bundle path, please open support ticket")
	}

	// Determine checkHost based on CLUSTER_ENV environment variable
	checkHost, err := getClusterEnv(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to determine checkHost: %w, please open support ticket", err)
	}
	if checkHost == "" {
		return nil, fmt.Errorf("failed to determine checkHost: empty checkHost, please open support ticket")
	}

	// Note: servicesDir is created by Kubernetes hostPath with DirectoryOrCreate
	sm := &SimpleManager{
		servicesDir:    DefaultServicesDir,
		initialPort:    InitialPort,
		portRange:      PortRange,
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		caFile:         caFile,
		checkHost:      checkHost,
		debugLevel:     DefaultDebugLevel,
		logger:         logger,
		debounceWindow: DefaultDebounceWindow,
	}

	// recoverExistingTunnels scans the services directory and rebuilds port allocation map
	if err := sm.recoverExistingTunnels(); err != nil {
		logger.Warn("Failed to rebuild port allocation map", zap.Error(err))
	}

	logger.Info("SimpleManager initialized",
		zap.String("servicesDir", sm.servicesDir),
		zap.Int("initialPort", sm.initialPort),
		zap.Int("portRange", sm.portRange),
		zap.Int("debugLevel", sm.debugLevel),
		zap.Duration("debounceWindow", sm.debounceWindow),
		zap.String("caFile", sm.caFile),
		zap.String("checkHost", sm.checkHost))

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

		if port >= sm.initialPort && port <= sm.initialPort+sm.portRange-1 {
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
	defer func() { // we have to handle the linter error file.close
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

		// Look for "accept" directive
		if strings.HasPrefix(line, "accept") {
			// Extract value after "accept" and "=" (handles "accept = value" or "accept=value")
			acceptValue := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "accept"), "="))

			// Extract and validate port
			port, err := sm.extractPortFromAddress(acceptValue)
			if err != nil {
				return 0, fmt.Errorf("line %d: %w", lineNum, err)
			}

			if port < sm.initialPort || port > sm.initialPort+sm.portRange-1 {
				return 0, fmt.Errorf("line %d: invalid port %d (range: %d-%d)", lineNum, port, sm.initialPort, sm.initialPort+sm.portRange-1)
			}

			return port, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("error reading config file %s: %w", configPath, err)
	}

	return 0, fmt.Errorf("no 'accept' directive found in config file %s", configPath)
}

// extractPortFromAddress extracts port number from address:port format
func (sm *SimpleManager) extractPortFromAddress(address string) (int, error) {
	// Find last colon for port (handles IPv4 and hostname formats)
	lastColon := strings.LastIndex(address, ":")
	if lastColon == -1 {
		return 0, fmt.Errorf("no port in address: %s", address)
	}

	port, err := strconv.Atoi(strings.TrimSpace(address[lastColon+1:]))
	if err != nil {
		return 0, fmt.Errorf("invalid port in %s: %w", address, err)
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

	// Find available port (doesn't commit to maps yet)
	port, err := sm.findAvailablePort(volumeID)
	if err != nil {
		return 0, err
	}

	// Write config file
	configPath := filepath.Join(sm.servicesDir, volumeID+".conf")
	config := sm.buildTunnelConfig(volumeID, nfsServer, port)
	if err := sm.writeTunnelConfig(configPath, config); err != nil {
		return 0, err
	}

	// Commit port allocation (file write succeeded)
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

	// Check if stunnel needs startup wait (only on first tunnel or after all removed)
	if !sm.stunnelStarted && !sm.isStunnelRunning() {
		sm.logger.Info("Stunnel not running, waiting for startup",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port),
			zap.Duration("waitTime", StunnelStartupWaitTime))
		time.Sleep(StunnelStartupWaitTime)

		if !sm.isStunnelRunning() {
			// Rollback: remove port allocation and config
			delete(sm.allocatedPorts, volumeID)
			delete(sm.portToVolume, port)
			_ = os.Remove(configPath) // #nosec G104: Best effort close, error not actionable
			return 0, fmt.Errorf("stunnel not running after %v wait, retry on mount failure", StunnelStartupWaitTime)
		}

		sm.stunnelStarted = true
		sm.logger.Info("Stunnel started successfully",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port))
	} else if sm.stunnelStarted {
		// Stunnel already running, schedule debounced SIGHUP
		sm.logger.Info("Scheduling debounced SIGHUP",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port))
		sm.scheduleDebouncedSIGHUP(requestID)
	} else {
		// Stunnel just confirmed running this case will be hit if csi node server pod is restarted, so we need to restore the state
		sm.stunnelStarted = true
		// Stunnel already running, schedule debounced SIGHUP
		sm.logger.Info("Scheduling debounced SIGHUP",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port))
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

	// Stop existing debounce timer before scheduling a new one.
	// For time.AfterFunc, Stop prevents the callback from running only if it has
	// not started yet. We do not drain timer.C here because AfterFunc callbacks do
	// not use timer channel consumption like time.NewTimer/time.After.
	if sm.debounceTimer != nil {
		sm.debounceTimer.Stop()
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

// buildTunnelConfig creates the stunnel service configuration content
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
		sm.logger.Warn("No port found for volume, config may already be removed",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID))
		return nil
	}

	// Check if any mounts are still using this tunnel port
	if sm.isTunnelPortInUse(tunnelPort) {
		sm.logger.Info("Tunnel port still in use by active mounts, keeping config",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", tunnelPort))
		return nil
	}

	configPath := filepath.Join(sm.servicesDir, volumeID+".conf")
	isLastTunnel := len(sm.allocatedPorts) == 1 // Check BEFORE deletion

	// Handle last tunnel cleanup BEFORE removing config
	if isLastTunnel {
		if err := sm.handleLastTunnelCleanup(volumeID, tunnelPort, requestID); err != nil {
			return err // Port maps NOT modified, safe to return
		}
	}

	// Release port from both maps
	delete(sm.allocatedPorts, volumeID)
	delete(sm.portToVolume, tunnelPort)

	// Remove config file
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		// Rollback: restore port mapping
		sm.allocatedPorts[volumeID] = tunnelPort
		sm.portToVolume[tunnelPort] = volumeID
		sm.logger.Error("Failed to remove config file, rolled back port release",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Error(err))
		return fmt.Errorf("failed to remove config: %w", err)
	}

	sm.logger.Info("Removed tunnel config",
		zap.String("RequestID", requestID),
		zap.String("volumeID", volumeID),
		zap.Int("port", tunnelPort),
		zap.Bool("isLastTunnel", isLastTunnel))

	// Schedule debounced SIGHUP for non-last tunnels (must release lock to avoid deadlock)
	if !isLastTunnel {
		sm.mu.Unlock()
		sm.scheduleDebouncedSIGHUP(requestID)
		sm.mu.Lock()
	}

	return nil
}

// handleLastTunnelCleanup handles the special case of removing the last tunnel
// Must be called with sm.mu held, temporarily releases it to avoid deadlock
func (sm *SimpleManager) handleLastTunnelCleanup(volumeID string, tunnelPort int, requestID string) error {
	sm.logger.Info("Last tunnel being removed, handling cleanup",
		zap.String("RequestID", requestID),
		zap.String("volumeID", volumeID))

	// Release sm.mu to acquire debounceMu (avoid deadlock)
	sm.mu.Unlock()
	defer sm.mu.Lock() // Re-acquire before returning

	// Cancel any pending debounced SIGHUP and send manual SIGHUP
	sm.debounceMu.Lock()
	defer sm.debounceMu.Unlock()

	if sm.debounceTimer != nil {
		sm.debounceTimer.Stop()
	}
	sm.pendingSIGHUP = false

	sm.logger.Info("Sending manual SIGHUP for last tunnel cleanup", zap.String("RequestID", requestID))

	// Send SIGHUP with last config still present
	if err := sm.reloadStunnel(requestID); err != nil {
		sm.logger.Error("Failed to send SIGHUP for last tunnel", zap.String("RequestID", requestID), zap.Error(err))
		return fmt.Errorf("failed to send SIGHUP before removing last config: %w", err)
	}

	// Wait for stunnel to complete reload
	sm.logger.Info("Waiting for stunnel reload to complete",
		zap.String("RequestID", requestID),
		zap.Duration("waitTime", StunnelReloadWaitTime))
	time.Sleep(StunnelReloadWaitTime)

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
// Two-phase commit: find port, write file, then commit to maps
func (sm *SimpleManager) findAvailablePort(volumeID string) (int, error) {
	maxPort := sm.initialPort + sm.portRange
	for port := sm.initialPort; port < maxPort; port++ {
		// Check if already allocated (O(1) lookup)
		if _, inUse := sm.portToVolume[port]; inUse {
			continue
		}

		// Verify system availability
		if !sm.isPortAvailable(port) {
			continue
		}

		sm.logger.Debug("Found available port", zap.Int("port", port), zap.String("volumeID", volumeID))
		return port, nil
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", sm.initialPort, maxPort-1)
}

// isPortAvailable checks if a port is available on the system
// Uses DialContext for fast check with timeout
func (sm *SimpleManager) isPortAvailable(port int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		_ = conn.Close() // #nosec G104: Best effort close, error not actionable
		return false     // Port in use
	}

	// Check for ECONNREFUSED (port available)
	if opErr, ok := err.(*net.OpError); ok {
		if sysErr, ok := opErr.Err.(*os.SyscallError); ok {
			return sysErr.Err == syscall.ECONNREFUSED
		}
	}

	return false // Uncertain state, assume unavailable for safety
}
