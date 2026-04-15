// Package stunnel provides a simple manager for denali-stunnel service configurations
package stunnel

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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
	logger         *zap.Logger
}

// Config holds configuration for SimpleManager
type Config struct {
	ServicesDir string
	BasePort    int
	PortRange   int
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

	// Ensure services directory exists
	if err := os.MkdirAll(servicesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create services directory: %w", err)
	}

	sm := &SimpleManager{
		servicesDir:    servicesDir,
		basePort:       basePort,
		portRange:      portRange,
		allocatedPorts: make(map[int]string),
		logger:         cfg.Logger,
	}

	// Recover existing tunnels from service configs
	if err := sm.recoverExistingTunnels(); err != nil {
		cfg.Logger.Warn("Failed to recover existing tunnels", zap.Error(err))
	}

	return sm, nil
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
func (sm *SimpleManager) EnsureTunnel(volumeID, nfsServer string) (int, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if volumeID == "" {
		return 0, fmt.Errorf("volumeID is required")
	}

	if nfsServer == "" {
		return 0, fmt.Errorf("nfsServer is required")
	}

	// Check if tunnel already exists
	configPath := sm.getConfigPath(volumeID)
	if _, err := os.Stat(configPath); err == nil {
		// Config exists, find its port
		for port, vid := range sm.allocatedPorts {
			if vid == volumeID {
				sm.logger.Info("Tunnel already exists",
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
	config := fmt.Sprintf(`[%s]
client = yes
accept = 127.0.0.1:%d
connect = %s:2049
transparent = none
`, volumeID, port, nfsServer)

	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		sm.releasePort(port)
		return 0, fmt.Errorf("failed to write config: %w", err)
	}

	sm.logger.Info("Created tunnel config",
		zap.String("volumeID", volumeID),
		zap.String("nfsServer", nfsServer),
		zap.Int("port", port),
		zap.String("configPath", configPath))

	return port, nil
}

// RemoveTunnel removes tunnel configuration
func (sm *SimpleManager) RemoveTunnel(volumeID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if volumeID == "" {
		return fmt.Errorf("volumeID is required")
	}

	configPath := sm.getConfigPath(volumeID)

	// Find and release port
	for port, vid := range sm.allocatedPorts {
		if vid == volumeID {
			sm.releasePort(port)
			break
		}
	}

	// Remove config file (denali-stunnel will auto-unload the service)
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove config: %w", err)
	}

	sm.logger.Info("Removed tunnel config",
		zap.String("volumeID", volumeID),
		zap.String("configPath", configPath))

	return nil
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
