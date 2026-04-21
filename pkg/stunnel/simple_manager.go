// Package stunnel provides a simple manager for denali-stunnel service configurations
package stunnel

import (
	"bufio"
	"fmt"
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
	DefaultBasePort = 20000

	// DefaultPortRange is the number of ports available
	DefaultPortRange = 10000
)

// SimpleManager manages stunnel service configs for denali-stunnel
type SimpleManager struct {
	mu             sync.RWMutex
	servicesDir    string
	basePort       int
	portRange      int
	allocatedPorts map[int]string // port -> volumeID
	caFile         string         // Path to CA bundle file
	logger         *zap.Logger
}

// Config holds configuration for SimpleManager
type Config struct {
	ServicesDir string
	BasePort    int
	PortRange   int
	CAFile      string // Path to CA bundle file for TLS verification
	Logger      *zap.Logger
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
	// Look in /etc/host-certs first (mounted from host), then fallback to container paths
	caFile := cfg.CAFile
	if caFile == "" {
		caFile = detectCABundle(cfg.Logger)
	}

	// Ensure services directory exists
	if err := os.MkdirAll(servicesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create services directory: %w", err)
	}

	sm := &SimpleManager{
		servicesDir:    servicesDir,
		basePort:       basePort,
		portRange:      portRange,
		allocatedPorts: make(map[int]string),
		caFile:         caFile,
		logger:         cfg.Logger,
	}

	// Recover existing tunnels from service configs
	if err := sm.recoverExistingTunnels(); err != nil {
		cfg.Logger.Warn("Failed to recover existing tunnels", zap.Error(err))
	}

	return sm, nil
}

// detectCABundle attempts to find the system CA bundle by checking common paths
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

	// Auto-detect CA bundle by checking common paths
	logger.Info("Auto-detecting CA bundle from common system paths")

	// Try RHEL/RHCOS path first (most common in enterprise/OpenShift)
	if _, err := os.Stat("/etc/host-certs/pki/ca-trust/extracted/pem/tls-ca-bundle.pem"); err == nil {
		logger.Info("Detected CA bundle", zap.String("path", "/etc/host-certs/pki/ca-trust/extracted/pem/tls-ca-bundle.pem"))
		return "/etc/host-certs/pki/ca-trust/extracted/pem/tls-ca-bundle.pem"
	}

	// Try Ubuntu/Debian path
	if _, err := os.Stat("/etc/host-certs/ssl/certs/ca-certificates.crt"); err == nil {
		logger.Info("Detected CA bundle", zap.String("path", "/etc/host-certs/ssl/certs/ca-certificates.crt"))
		return "/etc/host-certs/ssl/certs/ca-certificates.crt"
	}

	logger.Warn("No CA bundle detected, TLS verification will be disabled")
	return ""
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
			sm.allocatedPorts[port] = volumeID
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
	defer file.Close()

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
		// Config file exists, get port from map with read lock
		sm.mu.RLock()
		for port, vid := range sm.allocatedPorts {
			if vid == volumeID {
				sm.mu.RUnlock()
				sm.logger.Info("Tunnel already exists",
					zap.String("RequestID", requestID),
					zap.String("volumeID", volumeID),
					zap.Int("port", port))
				return port, nil
			}
		}
		sm.mu.RUnlock()
		// Config file exists but not in map - fall through to create
	}

	// Slow path: Need to create new tunnel with write lock
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double-check: another goroutine might have created it while we waited for lock
	if _, err := os.Stat(configPath); err == nil {
		for port, vid := range sm.allocatedPorts {
			if vid == volumeID {
				sm.logger.Info("Tunnel already exists (created by another goroutine)",
					zap.String("RequestID", requestID),
					zap.String("volumeID", volumeID),
					zap.Int("port", port))
				return port, nil
			}
		}
	}

	// Allocate new port
	port, err := sm.allocatePort(volumeID)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate port: %w", err)
	}

	// Create service config for denali-stunnel
	// VPC File Share uses TLS on port 20049
	var config string
	if sm.caFile != "" {
		// Use CA bundle for proper TLS verification (no self-signed certs)
		config = fmt.Sprintf(`[%s]
client = yes
accept = 127.0.0.1:%d
connect = %s:20049
CAfile = %s
verify = 2
`, volumeID, port, nfsServer, sm.caFile)
	} else {
		// Fallback: no CA verification (will accept self-signed certs)
		sm.logger.Warn("No CA bundle configured, TLS verification disabled",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID))
		config = fmt.Sprintf(`[%s]
client = yes
accept = 127.0.0.1:%d
connect = %s:20049
verify = 0
`, volumeID, port, nfsServer)
	}

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
		zap.String("caFile", sm.caFile),
		zap.Bool("tlsVerify", sm.caFile != ""))

	// Check if this is the first mount (first tunnel created)
	// For first mount: sleep 10 seconds to ensure stunnel is fully started and ready
	// For subsequent mounts: send SIGHUP for immediate reload
	isFirstMount := len(sm.allocatedPorts) == 1

	if isFirstMount {
		sm.logger.Info("First mount created, waiting 10 seconds for stunnel to start and load config",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port))
		// Sleep to ensure stunnel container is fully started and has loaded the config
		// This prevents exit code 129 when parallel mounts occur during stunnel startup
		time.Sleep(10 * time.Second)
		sm.logger.Info("First mount wait complete, stunnel should be ready",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port))
	} else {
		// stunnel is running, send SIGHUP for immediate reload
		sm.logger.Info("Subsequent mount, sending SIGHUP for immediate reload",
			zap.String("RequestID", requestID),
			zap.String("volumeID", volumeID),
			zap.Int("port", port))
		if err := sm.reloadStunnel(requestID); err != nil {
			sm.logger.Warn("Failed to send SIGHUP to stunnel, will rely on auto-detection",
				zap.String("RequestID", requestID),
				zap.String("volumeID", volumeID),
				zap.Error(err))
			// Don't fail - stunnel will pick it up via polling within 10 seconds
		}
	}

	return port, nil
}

// reloadStunnel sends SIGHUP to stunnel process to reload configuration
// This requires shareProcessNamespace: true in the pod spec to work across containers
func (sm *SimpleManager) reloadStunnel(requestID string) error {
	// Try to find stunnel process directly (most reliable)
	cmd := exec.Command("pgrep", "-x", "stunnel")
	output, err := cmd.Output()
	if err == nil {
		pidStr := strings.TrimSpace(string(output))
		if pid, err := strconv.Atoi(pidStr); err == nil {
			if err := syscall.Kill(pid, syscall.SIGHUP); err == nil {
				sm.logger.Info("Sent SIGHUP to stunnel process",
					zap.String("RequestID", requestID),
					zap.Int("pid", pid))
				return nil
			}
		}
	}

	// Fallback: try to signal the wrapper script (container init process)
	// The signal will propagate to child stunnel process
	cmd = exec.Command("pgrep", "-f", "run-stunnel.sh")
	output, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("stunnel container process not found (requires shareProcessNamespace: true): %w", err)
	}

	pidStr := strings.TrimSpace(string(output))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return fmt.Errorf("invalid PID: %w", err)
	}

	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Errorf("failed to send SIGHUP to container: %w", err)
	}

	sm.logger.Info("Sent SIGHUP to stunnel wrapper script",
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

	// Find the port for this volume
	var tunnelPort int
	for port, vid := range sm.allocatedPorts {
		if vid == volumeID {
			tunnelPort = port
			break
		}
	}

	if tunnelPort == 0 {
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

	// Remove config file (denali-stunnel will auto-unload the service)
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove config: %w", err)
	}

	sm.logger.Info("Removed tunnel config (no active mounts)",
		zap.String("RequestID", requestID),
		zap.String("volumeID", volumeID),
		zap.Int("port", tunnelPort),
		zap.String("configPath", configPath))

	// Check if services directory is now empty (last tunnel removed)
	isEmpty, err := sm.isServicesDirectoryEmpty()
	if err != nil {
		sm.logger.Warn("Failed to check if services directory is empty",
			zap.String("RequestID", requestID),
			zap.Error(err))
		isEmpty = false // Fail-safe: don't restart if we can't verify
	}

	if isEmpty {
		sm.logger.Info("Last tunnel removed (services directory empty), sending SIGINT to restart denali-stunnel container",
			zap.String("RequestID", requestID))
		// Send SIGINT to trigger container restart (requires restartPolicy: Always)
		if err := sm.terminateStunnel(requestID); err != nil {
			sm.logger.Warn("Failed to send SIGINT to stunnel container",
				zap.String("RequestID", requestID),
				zap.Error(err))
			// Don't fail - this is an optimization, not critical
		}
	} else {
		// Send SIGHUP to stunnel to reload configuration immediately
		if err := sm.reloadStunnel(requestID); err != nil {
			sm.logger.Warn("Failed to send SIGHUP to stunnel, will rely on auto-detection",
				zap.String("RequestID", requestID),
				zap.Error(err))
			// Don't fail - stunnel will pick it up via polling within 10 seconds
		}
	}

	return nil
}

// isServicesDirectoryEmpty checks if the services directory has no .conf files
func (sm *SimpleManager) isServicesDirectoryEmpty() (bool, error) {
	files, err := os.ReadDir(sm.servicesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}

	// Check if any .conf files exist
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".conf" {
			return false, nil
		}
	}

	return true, nil
}

// terminateStunnel sends SIGTERM to stunnel process to trigger container restart
// This is used when the last tunnel is removed to clean up resources
func (sm *SimpleManager) terminateStunnel(requestID string) error {
	// Find stunnel process (visible via shareProcessNamespace)
	// Note: run-stunnel.sh is in a different PID namespace and not visible
	cmd := exec.Command("pgrep", "-x", "stunnel")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("stunnel process not found (requires shareProcessNamespace: true): %w", err)
	}

	pidStr := strings.TrimSpace(string(output))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return fmt.Errorf("invalid PID: %w", err)
	}

	// Send SIGTERM to stunnel for graceful shutdown
	// This will cause the container to exit and restart (with restartPolicy: Always)
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to stunnel: %w", err)
	}

	sm.logger.Info("Sent SIGTERM to stunnel for container restart",
		zap.String("RequestID", requestID),
		zap.Int("pid", pid))
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

// GetTunnelPort returns the port for a volume if it exists
func (sm *SimpleManager) GetTunnelPort(volumeID string) (int, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for port, vid := range sm.allocatedPorts {
		if vid == volumeID {
			return port, true
		}
	}
	return 0, false
}

// allocatePort finds an available port
func (sm *SimpleManager) allocatePort(volumeID string) (int, error) {
	for i := 0; i < sm.portRange; i++ {
		port := sm.basePort + i
		if _, used := sm.allocatedPorts[port]; !used {
			sm.allocatedPorts[port] = volumeID
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", sm.basePort, sm.basePort+sm.portRange-1)
}

// releasePort frees a port
func (sm *SimpleManager) releasePort(port int) {
	delete(sm.allocatedPorts, port)
}

// getConfigPath returns the path to the config file for a volume
func (sm *SimpleManager) getConfigPath(volumeID string) string {
	return filepath.Join(sm.servicesDir, volumeID+".conf")
}

// Cleanup removes all tunnel configs (for shutdown)
func (sm *SimpleManager) Cleanup() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

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

		configPath := filepath.Join(sm.servicesDir, file.Name())
		if err := os.Remove(configPath); err != nil {
			sm.logger.Warn("Failed to remove config file",
				zap.String("file", file.Name()),
				zap.Error(err))
		}
	}

	sm.allocatedPorts = make(map[int]string)
	sm.logger.Info("Cleaned up all tunnel configs")

	return nil
}

// GetAllocatedPortsCount returns the number of currently allocated ports
func (sm *SimpleManager) GetAllocatedPortsCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.allocatedPorts)
}

// Made with Bob
