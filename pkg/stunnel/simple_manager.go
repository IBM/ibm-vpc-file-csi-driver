// Package stunnel provides a simple manager for denali-stunnel service configurations
package stunnel

import (
	"bufio"
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

	// DefaultBasePort is the starting port for tunnel allocation
	DefaultBasePort = 10001

	// DefaultPortRange is the number of ports available
	DefaultPortRange = 20000

	// DefaultLogDir is where stunnel writes per-volume log files
	DefaultLogDir = "/var/log/stunnel"

	// DefaultDebugLevel is the stunnel debug verbosity (0-7, 5 recommended for production)
	DefaultDebugLevel = 4
)

// SimpleManager manages stunnel service configs for denali-stunnel
type SimpleManager struct {
	mu             sync.RWMutex
	servicesDir    string
	basePort       int
	portRange      int
	allocatedPorts map[string]int // volumeID -> port (O(1) lookup by volumeID)
	caFile         string         // Path to CA bundle file
	checkHost      string         // Hostname for TLS certificate verification
	stunnelStarted bool           // Tracks if stunnel has been confirmed running
	logDir         string         // Directory for stunnel log files
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
	BasePort       int
	PortRange      int
	CAFile         string // Path to CA bundle file for TLS verification
	LogDir         string // Directory for stunnel log files (default: /var/log/stunnel)
	DebugLevel     int    // Stunnel debug level 0-7 (default: 5)
	Logger         *zap.Logger
	DebounceWindow time.Duration // Time window to collect multiple SIGHUPs (default: 2s)
}

// NewSimpleManager creates a new SimpleManager
func NewSimpleManager(cfg *Config) (*SimpleManager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if cfg.Logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	servicesDir := cfg.ServicesDir
	if servicesDir == "" {
		servicesDir = DefaultServicesDir
	}

	basePort := cfg.BasePort
	if basePort == 0 {
		basePort = DefaultBasePort
	}

	portRange := cfg.PortRange
	if portRange == 0 {
		portRange = DefaultPortRange
	}

	// Auto-detect CA bundle if not provided
	// Use OS_TYPE environment variable to determine the correct path
	caFile := cfg.CAFile
	if caFile == "" {
		caFile = detectCABundle(cfg.Logger)
	}

	// Determine checkHost based on CLUSTER_ENV environment variable
	checkHost := getCheckHost(cfg.Logger)

	// Set log directory (created by Kubernetes hostPath with DirectoryOrCreate)
	logDir := cfg.LogDir
	if logDir == "" {
		logDir = DefaultLogDir
	}

	// Set debug level
	debugLevel := cfg.DebugLevel
	if debugLevel == 0 {
		debugLevel = DefaultDebugLevel
	}
	// Validate debug level (0-7)
	if debugLevel < 0 || debugLevel > 7 {
		cfg.Logger.Warn("Invalid stunnel debug level, using default",
			zap.Int("provided", debugLevel),
			zap.Int("default", DefaultDebugLevel))
		debugLevel = DefaultDebugLevel
	}

	// Note: Both servicesDir and logDir are created by Kubernetes hostPath with DirectoryOrCreate
	// No need to create them here

	// Set default debounce window if not provided
	debounceWindow := cfg.DebounceWindow
	if debounceWindow == 0 {
		debounceWindow = 2 * time.Second // Default: 2 seconds
	}

	sm := &SimpleManager{
		servicesDir:    servicesDir,
		basePort:       basePort,
		portRange:      portRange,
		allocatedPorts: make(map[string]int),
		caFile:         caFile,
		checkHost:      checkHost,
		logDir:         logDir,
		debugLevel:     debugLevel,
		logger:         cfg.Logger,
		debounceWindow: debounceWindow,
	}

	// Recover existing tunnels from service configs
	if err := sm.recoverExistingTunnels(); err != nil {
		cfg.Logger.Warn("Failed to recover existing tunnels", zap.Error(err))
	}

	cfg.Logger.Info("SimpleManager initialized with SIGHUP debouncing",
		zap.Duration("debounceWindow", debounceWindow),
		zap.String("logDir", logDir),
		zap.Int("debugLevel", debugLevel))

	return sm, nil
}

// detectCABundle attempts to find the system CA bundle based on OS_TYPE environment variable
func detectCABundle(logger *zap.Logger) string {
	// Check for explicit override first
	if caFile := os.Getenv("STUNNEL_CA_FILE"); caFile != "" {
		if _, err := os.Stat(caFile); err == nil {
			logger.Info("Using CA bundle from STUNNEL_CA_FILE env var", zap.String("path", caFile))
			return caFile
		}
		logger.Warn("STUNNEL_CA_FILE specified but file not found",
			zap.String("path", caFile))
	}

	// Use OS_TYPE environment variable to determine CA bundle path
	osType := os.Getenv("OS_TYPE")
	if osType == "" {
		osType = "RHCOS" // Default to RHCOS if not specified
	}

	logger.Info("Detecting CA bundle based on OS type",
		zap.String("osType", osType))

	var caPath string
	switch osType {
	case "RHCOS", "RHEL":
		// RHEL/RHCOS path (most common in enterprise/OpenShift)
		caPath = "/etc/host-certs/pki/ca-trust/extracted/pem/tls-ca-bundle.pem"
	case "Ubuntu", "Debian":
		// Ubuntu/Debian path
		caPath = "/etc/host-certs/ssl/certs/ca-certificates.crt"
	default:
		logger.Warn("Unknown OS_TYPE, trying RHCOS path",
			zap.String("osType", osType))
		caPath = "/etc/host-certs/pki/ca-trust/extracted/pem/tls-ca-bundle.pem"
	}

	logger.Info("Using CA bundle path",
		zap.String("path", caPath),
		zap.String("osType", osType))
	return caPath
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
func (sm *SimpleManager) extractPortFromConfigFile(configPath string) (int, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			sm.logger.Warn("Failed to close config file", zap.String("path", configPath), zap.Error(closeErr))
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Look for "accept = 127.0.0.1:PORT"
		if strings.HasPrefix(line, "accept") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				portStr := strings.TrimSpace(parts[1])
				if port, err := strconv.Atoi(portStr); err == nil {
					return port, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("port not found in config")
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
	configPath := sm.getConfigPath(volumeID)
	if _, err := os.Stat(configPath); err == nil {
		// Config file exists, get port from map with read lock (O(1) lookup)
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
		// Config file exists but not in map - fall through to create
	}

	// Slow path: Need to create new tunnel with write lock
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double-check: another goroutine might have created it while we waited for lock (O(1) lookup)
	if _, err := os.Stat(configPath); err == nil {
		if port, exists := sm.allocatedPorts[volumeID]; exists {
			sm.logger.Info("Tunnel already exists (created by another goroutine)",
				zap.String("RequestID", requestID),
				zap.String("volumeID", volumeID),
				zap.Int("port", port))
			return port, nil
		}
	}

	// Allocate new port
	port, err := sm.allocatePort(volumeID)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate port: %w", err)
	}

	// Create service config for denali-stunnel
	// VPC File Share uses TLS on port 20049
	// SECURITY: Always require proper TLS verification - fail if CA bundle or checkHost not configured
	if sm.caFile == "" || sm.checkHost == "" {
		sm.releasePort(port)
		return 0, fmt.Errorf("TLS verification required but CA bundle or checkHost not configured (caFile=%s, checkHost=%s) - refusing to create insecure tunnel", sm.caFile, sm.checkHost)
	}

	// Use CA bundle and checkHost for proper TLS verification
	config := fmt.Sprintf(`[%s]
client = yes
accept = 127.0.0.1:%d
connect = %s:20049
CAfile = %s
checkHost = %s
verify = 2
debug = %d
`, volumeID, port, nfsServer, sm.caFile, sm.checkHost, sm.debugLevel)

	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		sm.releasePort(port)
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
		sm.logger.Info("Stunnel not running, waiting 10 seconds for stunnel to start and load config",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port),
			zap.Int("allocatedPorts", len(sm.allocatedPorts)))
		// Sleep to ensure stunnel container is fully started and has loaded the config
		// This prevents exit code 129 when trying to signal a non-existent process
		time.Sleep(10 * time.Second)
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

	// If timer already exists, reset it
	if sm.debounceTimer != nil {
		sm.debounceTimer.Stop()
	}

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
			zap.String("RequestID", requestID),
			zap.Duration("debounceWindow", sm.debounceWindow))

		if err := sm.reloadStunnel(requestID); err != nil {
			sm.logger.Warn("Failed to send debounced SIGHUP to stunnel",
				zap.String("RequestID", requestID),
				zap.Error(err))
			// Don't fail - stunnel will pick it up via next debounce SIGHUP window
		} else {
			sm.logger.Info("Successfully sent debounced SIGHUP to stunnel",
				zap.String("RequestID", requestID))
		}
	})

	sm.logger.Info("SIGHUP debounced, will send after window",
		zap.String("RequestID", requestID),
		zap.Duration("debounceWindow", sm.debounceWindow))
}

// isStunnelRunning checks if stunnel process is currently running
func (sm *SimpleManager) isStunnelRunning() bool {
	// Try to find stunnel process
	cmd := exec.Command("pgrep", "-x", "stunnel")
	err := cmd.Run()
	return err == nil
}

// reloadStunnel sends SIGHUP to stunnel process to reload configuration
// This requires shareProcessNamespace: true in the pod spec to work across containers
// NOTE: Only signals the stunnel process directly, NOT the wrapper script (run-stunnel.sh)
// Signaling the wrapper script causes exit code 129 and container restart
func (sm *SimpleManager) reloadStunnel(requestID string) error {
	// Find stunnel process directly
	cmd := exec.Command("pgrep", "-x", "stunnel")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("stunnel process not found (requires shareProcessNamespace: true): %w", err)
	}

	pidStr := strings.TrimSpace(string(output))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return fmt.Errorf("invalid stunnel PID: %w", err)
	}

	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Errorf("failed to send SIGHUP to stunnel process: %w", err)
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
	if isLastTunnel {
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

			// Wait for stunnel to complete the reload (~4 seconds)
			// This ensures all old listeners are cleaned up before we remove the last config
			time.Sleep(5 * time.Second)
		} else {
			sm.debounceMu.Unlock()
		}
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
		sm.scheduleDebouncedSIGHUP(requestID)
	} else {
		sm.logger.Info("Last tunnel removed, skipping final SIGHUP",
			zap.String("RequestID", requestID))
		// Note: stunnel process keeps running (not killed/restarted)
		// stunnelStarted remains true, so next mount will use debounced SIGHUP route
	}

	return nil
}

// isTunnelPortInUse checks if any NFS mounts are using the specified tunnel port
// by reading /proc/mounts. This is fast (~2ms) and won't hang even if NFS is unresponsive.
func (sm *SimpleManager) isTunnelPortInUse(port int) bool {
	// Read /proc/mounts directly - doesn't stat the filesystem, so won't hang
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		sm.logger.Warn("Failed to read /proc/mounts, assuming port not in use",
			zap.Int("port", port),
			zap.Error(err))
		return false // Fail-safe: allow tunnel removal if we can't check
	}

	// Search for mounts using this port
	// Mount entries look like: 127.0.0.1:/EXPORT /mountpoint nfs4 rw,...,port=20000,... 0 0
	portStr := fmt.Sprintf("port=%d", port)
	lines := strings.Split(string(data), "\n")
	mountCount := 0

	for _, line := range lines {
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
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	port, exists := sm.allocatedPorts[volumeID]
	return port, exists
}

// allocatePort finds an available port
// Checks both internal map and actual port availability on the system
func (sm *SimpleManager) allocatePort(volumeID string) (int, error) {
	for i := 0; i < sm.portRange; i++ {
		port := sm.basePort + i

		// Check if port is already allocated to any volume (O(n) check, but only during allocation)
		portInUse := false
		for _, allocatedPort := range sm.allocatedPorts {
			if allocatedPort == port {
				portInUse = true
				break
			}
		}
		if portInUse {
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

		// Port is available both in map and on system
		sm.allocatedPorts[volumeID] = port
		sm.logger.Debug("Allocated port",
			zap.Int("port", port),
			zap.String("volumeID", volumeID))
		return port, nil
	}
	return 0, fmt.Errorf("no available ports in range %d-%d (all ports in use or allocated)", sm.basePort, sm.basePort+sm.portRange-1)
}

// isPortAvailable checks if a port is actually available on the system
// by attempting to bind to it. This prevents conflicts with other processes.
func (sm *SimpleManager) isPortAvailable(port int) bool {
	// Try to bind to the port to verify it's available
	// Use net.Listen which will fail if port is in use
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		// Port is in use or unavailable
		return false
	}

	// Port is available, close the listener immediately
	if err := listener.Close(); err != nil {
		sm.logger.Warn("Failed to close test listener", zap.Int("port", port), zap.Error(err))
	}
	return true
}

// releasePort frees a port by removing the volumeID mapping
func (sm *SimpleManager) releasePort(port int) {
	// Find and delete the volumeID that has this port
	for volumeID, allocatedPort := range sm.allocatedPorts {
		if allocatedPort == port {
			delete(sm.allocatedPorts, volumeID)
			return
		}
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
