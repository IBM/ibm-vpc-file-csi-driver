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

// Package stunnel provides a simple manager for denali-stunnel service configurations
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
	// DefaultServicesDir is where denali-stunnel reads service configs
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
)

// SimpleManager manages stunnel service configs for denali-stunnel
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
	debounceWindow := 2 * time.Second

	// Auto-detect CA bundle based on OS_TYPE environment variable
	caFile, err := detectCABundle(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to detect CA bundle: %w", err)
	}

	// Determine checkHost based on CLUSTER_ENV environment variable
	checkHost := getCheckHost(logger)

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

	// Recover existing tunnels from service configs
	if err := sm.recoverExistingTunnels(); err != nil {
		logger.Warn("Failed to recover existing tunnels if any. Please restart the CSI node server POD to refresh again.", zap.Error(err))
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
		caPath = "/etc/host-certs/pki/ca-trust/extracted/pem/tls-ca-bundle.pem"
	case "Ubuntu":
		// Ubuntu path
		caPath = "/etc/host-certs/ssl/certs/ca-certificates.crt"
	default:
		return "", fmt.Errorf("unknown OS_TYPE: %s (supported: RHCOS, RHEL, Ubuntu)", osType)
	}

	logger.Info("Detected CA bundle path based on OS type",
		zap.String("path", caPath),
		zap.String("osType", osType))
	return caPath, nil
}

// getCheckHost determines the hostname for TLS certificate verification based on CLUSTER_ENV
func getCheckHost(logger *zap.Logger) string {
	// Use CLUSTER_ENV environment variable to determine checkHost
	clusterEnv := os.Getenv("CLUSTER_ENV")
	if clusterEnv == "" {
		clusterEnv = "production" // Default to production if not specified
	}

	var checkHost string
	switch clusterEnv {
	case "production", "prod":
		checkHost = "production.is-share.appdomain.cloud"
	case "staging", "stage":
		checkHost = "staging.is-share.appdomain.cloud"
	default:
		logger.Warn("Unknown CLUSTER_ENV, defaulting to production",
			zap.String("clusterEnv", clusterEnv))
		checkHost = "production.is-share.appdomain.cloud"
	}

	logger.Info("Determined checkHost for TLS verification",
		zap.String("checkHost", checkHost),
		zap.String("clusterEnv", clusterEnv))
	return checkHost
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
//   - accept = [::1]:PORT (IPv6)
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

			// Validate port range (1-65535)
			if port < 1 || port > 65535 {
				return 0, fmt.Errorf("line %d: invalid port %d (must be 1-65535)", lineNum, port)
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

// extractPortFromAddress extracts port number from various address formats
// Supports: IPv4 (127.0.0.1:PORT), IPv6 ([::1]:PORT), hostname (host:PORT)
func (sm *SimpleManager) extractPortFromAddress(address string, lineNum int) (int, error) {
	// Handle IPv6 format: [::1]:PORT or [2001:db8::1]:PORT
	if strings.HasPrefix(address, "[") {
		closeBracket := strings.Index(address, "]")
		if closeBracket == -1 {
			return 0, fmt.Errorf("malformed IPv6 address (missing closing bracket): %s", address)
		}
		// Extract port after ]:
		if closeBracket+1 >= len(address) || address[closeBracket+1] != ':' {
			return 0, fmt.Errorf("malformed IPv6 address (missing port after ]): %s", address)
		}
		portStr := strings.TrimSpace(address[closeBracket+2:])
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return 0, fmt.Errorf("invalid port in IPv6 address %s: %w", address, err)
		}
		return port, nil
	}

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
		return 0, fmt.Errorf("nfsServer is required")
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

	// Allocate new port
	port, err := sm.allocatePort(volumeID)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate port: %w", err)
	}

	// Get config path for this volume
	configPath := sm.getConfigPath(volumeID)

	// Create service config for denali-stunnel
	// VPC File Share uses TLS on NFSOverTLSPort
	// SECURITY: Always require proper TLS verification - fail if CA bundle or checkHost not configured
	if sm.caFile == "" || sm.checkHost == "" {
		sm.releasePort(port)
		return 0, fmt.Errorf("TLS verification required but CA bundle or checkHost not configured (caFile=%s, checkHost=%s) - refusing to create insecure tunnel", sm.caFile, sm.checkHost)
	}

	// Use CA bundle and checkHost for proper TLS verification
	config := fmt.Sprintf(`[%s]
client = yes
accept = 127.0.0.1:%d
connect = %s:%d
CAfile = %s
checkHost = %s
verify = 2
debug = %d
`, volumeID, port, nfsServer, NFSOverTLSPort, sm.caFile, sm.checkHost, sm.debugLevel)

	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		sm.releasePort(port)
		// Clean up config file if it was partially written
		if removeErr := os.Remove(configPath); removeErr != nil && !os.IsNotExist(removeErr) {
			sm.logger.Warn("Failed to clean up config file after write error",
				zap.String("configPath", configPath),
				zap.Error(removeErr))
		}
		return 0, fmt.Errorf("failed to write config: %w", err)
	}

	sm.logger.Info("Created tunnel config",
		zap.String("RequestID", requestID),
		zap.String("volumeID", volumeID),
		zap.String("nfsServer", nfsServer),
		zap.Int("port", port),
		zap.String("configPath", configPath),
		zap.Int("debugLevel", sm.debugLevel),
		zap.String("caFile", sm.caFile),
		zap.String("checkHost", sm.checkHost),
		zap.Bool("tlsVerify", sm.caFile != "" && sm.checkHost != ""))

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
		sm.stunnelStarted = true
		sm.logger.Info("Wait complete, stunnel should be ready",
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
		// Provide clear error message about configuration requirement
		return fmt.Errorf("stunnel process not found - this typically means shareProcessNamespace is not enabled in the pod spec. Ensure the node server pod has 'shareProcessNamespace: true' configured: %w", err)
	}

	pidStr := strings.TrimSpace(string(output))
	if len(pidStr) == 0 {
		return fmt.Errorf("no stunnel PIDs found in pgrep output")
	}

	// Check for multiple PIDs (pgrep returns one PID per line)
	// If there's a newline, we have multiple processes
	if strings.Contains(pidStr, "\n") {
		pidLines := strings.Split(pidStr, "\n")
		var pids []string
		for _, line := range pidLines {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				pids = append(pids, trimmed)
			}
		}
		sm.logger.Error("Multiple stunnel processes detected - abnormal state, restart required",
			zap.String("RequestID", requestID),
			zap.Strings("pids", pids),
			zap.Int("count", len(pids)))
		return fmt.Errorf("multiple stunnel processes detected (%d PIDs: %v) - this is an abnormal state, please restart the node server pod to recover", len(pids), pids)
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
		return fmt.Errorf("failed to send SIGHUP to stunnel process (PID %d): %w", pid, err)
	}

	sm.logger.Info("Sent SIGHUP to stunnel process",
		zap.String("RequestID", requestID),
		zap.Int("pid", pid))
	return nil
}

// RemoveTunnel removes tunnel configuration only if no active mounts use it
func (sm *SimpleManager) RemoveTunnel(volumeID, requestID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if volumeID == "" {
		return fmt.Errorf("volumeID is required")
	}

	// Find the port for this volume (O(1) lookup)
	tunnelPort, exists := sm.allocatedPorts[volumeID]
	if !exists {
		sm.logger.Warn("No port found for volume, config may already be removed",
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

	configPath := sm.getConfigPath(volumeID)

	// Release port
	sm.releasePort(tunnelPort)

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
			sm.debounceMu.Unlock()

			// Fire SIGHUP immediately to reload with just this last config
			// This cleans up all other listeners before we remove the last config
			if err := sm.reloadStunnel(requestID); err != nil {
				sm.logger.Error("Failed to reload stunnel for last tunnel removal",
					zap.String("requestID", requestID),
					zap.Error(err))
			}

			// Wait for stunnel to complete the reload
			// This ensures all old listeners are cleaned up before we remove the last config
			// Note: This sleep occurs with NO locks held, so it doesn't block other operations
			time.Sleep(StunnelReloadWaitTime)
		} else {
			sm.debounceMu.Unlock()
		}

		// Re-acquire sm.mu before continuing
		sm.mu.Lock()
	}

	// Remove config file (denali-stunnel will auto-unload the service)
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
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

		// Unlock will be handled by defer at function exit
		// We need to schedule SIGHUP after releasing the lock to avoid deadlock
		// So we'll unlock via defer, then schedule
		defer sm.scheduleDebouncedSIGHUP(requestID)
		return nil
	}

	sm.logger.Info("Last tunnel removed, skipping final SIGHUP",
		zap.String("RequestID", requestID))
	// Note: stunnel process keeps running (not killed/restarted)
	// stunnelStarted remains true, so next mount will use debounced SIGHUP route

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

// allocatePort finds an available port
// Checks both internal map and actual port availability on the system
// Uses reverse map for O(1) port lookup instead of O(n) iteration
func (sm *SimpleManager) allocatePort(volumeID string) (int, error) {
	for i := 0; i < sm.portRange; i++ {
		port := sm.initialPort + i

		// Check if port is already allocated using reverse map (O(1) lookup)
		if _, portInUse := sm.portToVolume[port]; portInUse {
			continue
		}

		// Verify port is actually available on the system
		// This prevents conflicts with other processes or stale state
		if !sm.isPortAvailable(port) {
			sm.logger.Warn("Port in use by another process, skipping",
				zap.Int("port", port),
				zap.String("volumeID", volumeID))
			continue
		}

		// Port is available both in map and on system - update both maps
		sm.allocatedPorts[volumeID] = port
		sm.portToVolume[port] = volumeID
		sm.logger.Debug("Allocated port",
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
	// Fast validation of port range
	if port < 1024 || port > 65535 {
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

// releasePort frees a port by removing both forward and reverse mappings
func (sm *SimpleManager) releasePort(port int) {
	// Use reverse map for O(1) lookup of volumeID
	if volumeID, exists := sm.portToVolume[port]; exists {
		delete(sm.allocatedPorts, volumeID)
		delete(sm.portToVolume, port)
	}
}

// getConfigPath returns the path to the config file for a volume
func (sm *SimpleManager) getConfigPath(volumeID string) string {
	return filepath.Join(sm.servicesDir, volumeID+".conf")
}

// GetAllocatedPortsCount returns the number of currently allocated ports
func (sm *SimpleManager) GetAllocatedPortsCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.allocatedPorts)
}

// Made with Bob
