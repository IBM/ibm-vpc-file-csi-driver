package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// DefaultSocketPath is the node-local Unix domain socket path for tunnel manager API
	DefaultSocketPath = "/var/lib/kubelet/plugins/vpc.file.csi.ibm.io/tunnel-manager.sock"
)

// TunnelInfo is the transport-safe view of a tunnel
type TunnelInfo struct {
	VolumeID   string `json:"volumeID"`
	RemoteAddr string `json:"remoteAddr"`
	LocalPort  int    `json:"localPort"`
	State      string `json:"state"`
	RefCount   int    `json:"refCount"`
}

// Service defines the node-local tunnel manager API used by the CSI node service
type Service interface {
	EnsureTunnel(ctx context.Context, volumeID, nfsServer string) (*TunnelInfo, error)
	RemoveTunnel(ctx context.Context, volumeID string) error
	GetTunnel(ctx context.Context, volumeID string) (*TunnelInfo, bool, error)
	Health(ctx context.Context) error
}

const (
	// DefaultBasePort is the starting port for tunnel allocation
	DefaultBasePort = 20000
	// DefaultPortRange is the number of ports available for allocation
	DefaultPortRange = 10000
	// DefaultConfigDir is the directory for stunnel configurations
	DefaultConfigDir = "/etc/stunnel"
	// DefaultCAFile is the system CA bundle path (mounted from host)
	DefaultCAFile = "/host-certs/tls-ca-bundle.pem"
	// DefaultNFSPort is the NFS port for RFS shares
	DefaultNFSPort = 20049
	// DefaultHealthCheckInterval is how often to check tunnel health
	DefaultHealthCheckInterval = 30 * time.Second
)

// TunnelState represents the current state of a tunnel
type TunnelState string

const (
	StateStarting TunnelState = "starting"
	StateRunning  TunnelState = "running"
	StateFailed   TunnelState = "failed"
	StateStopped  TunnelState = "stopped"
)

// Tunnel represents a single Stunnel instance for a volume
type Tunnel struct {
	VolumeID     string
	RemoteAddr   string
	LocalPort    int
	Cmd          *exec.Cmd
	ConfigPath   string
	State        TunnelState
	LastHealthy  time.Time
	RefCount     int // Number of active mounts using this tunnel
	RestartCount int
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *zap.Logger
}

// Manager manages multiple Stunnel instances
type Manager struct {
	mu             sync.RWMutex
	tunnels        map[string]*Tunnel
	basePort       int
	portRange      int
	configDir      string
	caFile         string
	nfsPort        int
	environment    string // "staging" or "production"
	healthInterval time.Duration
	allocatedPorts map[int]bool
	logger         *zap.Logger
	healthStop     chan struct{}
	wg             sync.WaitGroup
}

// Config holds configuration for the tunnel manager
type Config struct {
	BasePort       int
	PortRange      int
	ConfigDir      string
	CAFile         string
	NFSPort        int
	Environment    string
	HealthInterval time.Duration
	Logger         *zap.Logger
}

// NewManager creates a new tunnel manager with the given configuration
func NewManager(cfg *Config) (*Manager, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	// Set defaults
	if cfg.BasePort == 0 {
		cfg.BasePort = DefaultBasePort
	}
	if cfg.PortRange == 0 {
		cfg.PortRange = DefaultPortRange
	}
	if cfg.ConfigDir == "" {
		cfg.ConfigDir = DefaultConfigDir
	}
	if cfg.CAFile == "" {
		cfg.CAFile = DefaultCAFile
	}
	if cfg.NFSPort == 0 {
		cfg.NFSPort = DefaultNFSPort
	}
	if cfg.Environment == "" {
		cfg.Environment = "production"
	}
	if cfg.HealthInterval == 0 {
		cfg.HealthInterval = DefaultHealthCheckInterval
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}

	// Ensure config directory exists
	if err := os.MkdirAll(cfg.ConfigDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	m := &Manager{
		tunnels:        make(map[string]*Tunnel),
		basePort:       cfg.BasePort,
		portRange:      cfg.PortRange,
		configDir:      cfg.ConfigDir,
		caFile:         cfg.CAFile,
		nfsPort:        cfg.NFSPort,
		environment:    cfg.Environment,
		healthInterval: cfg.HealthInterval,
		allocatedPorts: make(map[int]bool),
		logger:         cfg.Logger,
		healthStop:     make(chan struct{}),
	}

	// Start health check routine
	m.wg.Add(1)
	go m.healthCheckLoop()

	m.logger.Info("Tunnel manager initialized",
		zap.Int("basePort", m.basePort),
		zap.Int("portRange", m.portRange),
		zap.String("configDir", m.configDir),
		zap.String("environment", m.environment))

	return m, nil
}

// TunnelMetadata stores persistent information about a tunnel
type TunnelMetadata struct {
	VolumeID  string `json:"volumeID"`
	NFSServer string `json:"nfsServer"`
	Port      int    `json:"port"`
	RefCount  int    `json:"refCount"`
}

const (
	metadataSaveRetryCount = 3
	metadataSaveRetryDelay = 200 * time.Millisecond
)

func (m *Manager) metadataPath(volumeID string) string {
	return filepath.Join(m.configDir, fmt.Sprintf("%s.meta.json", volumeID))
}

func (m *Manager) metadataBackupPath(volumeID string) string {
	return filepath.Join(m.configDir, fmt.Sprintf("%s.meta.json.bak", volumeID))
}

func (m *Manager) validateTunnelMetadata(metadata *TunnelMetadata) error {
	if metadata == nil {
		return fmt.Errorf("metadata is nil")
	}
	if metadata.VolumeID == "" {
		return fmt.Errorf("metadata volumeID is empty")
	}
	if metadata.NFSServer == "" {
		return fmt.Errorf("metadata nfsServer is empty")
	}
	if metadata.Port < m.basePort || metadata.Port >= m.basePort+m.portRange {
		return fmt.Errorf("metadata port %d is outside managed range %d-%d", metadata.Port, m.basePort, m.basePort+m.portRange-1)
	}
	if metadata.RefCount < 0 {
		return fmt.Errorf("metadata refCount %d is invalid", metadata.RefCount)
	}
	return nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmpPath := path + ".tmp"

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	writeErr := func() error {
		if _, err := f.Write(data); err != nil {
			return err
		}
		if err := f.Sync(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return os.Rename(tmpPath, path)
	}()

	if writeErr != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return writeErr
	}

	return nil
}

func (m *Manager) saveTunnelMetadataWithRetry(t *Tunnel) error {
	var lastErr error

	for attempt := 1; attempt <= metadataSaveRetryCount; attempt++ {
		err := m.saveTunnelMetadata(t)
		if err == nil {
			if attempt > 1 {
				m.logger.Info("Tunnel metadata save succeeded after retry",
					zap.String("volumeID", t.VolumeID),
					zap.Int("attempt", attempt))
			}
			return nil
		}

		lastErr = err
		if attempt < metadataSaveRetryCount {
			retryDelay := metadataSaveRetryDelay * time.Duration(attempt)
			m.logger.Warn("Tunnel metadata save failed, retrying",
				zap.String("volumeID", t.VolumeID),
				zap.Int("attempt", attempt),
				zap.Int("maxAttempts", metadataSaveRetryCount),
				zap.Duration("retryDelay", retryDelay),
				zap.Error(err))
			time.Sleep(retryDelay)
		}
	}

	return fmt.Errorf("failed to save tunnel metadata after %d attempts: %w", metadataSaveRetryCount, lastErr)
}

// saveTunnelMetadata persists tunnel metadata to disk
func (m *Manager) saveTunnelMetadata(t *Tunnel) error {
	metadata := TunnelMetadata{
		VolumeID:  t.VolumeID,
		NFSServer: t.RemoteAddr,
		Port:      t.LocalPort,
		RefCount:  t.RefCount,
	}

	if err := m.validateTunnelMetadata(&metadata); err != nil {
		return fmt.Errorf("refusing to save invalid metadata: %w", err)
	}

	metadataPath := m.metadataPath(t.VolumeID)
	backupPath := m.metadataBackupPath(t.VolumeID)

	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := atomicWriteFile(metadataPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	if err := atomicWriteFile(backupPath, data, 0600); err != nil {
		m.logger.Warn("Failed to write metadata backup",
			zap.String("volumeID", t.VolumeID),
			zap.String("backupPath", backupPath),
			zap.Error(err))
	}

	return nil
}

// loadTunnelMetadata loads tunnel metadata from disk
func (m *Manager) loadTunnelMetadata(volumeID string) (*TunnelMetadata, error) {
	metadataPath := m.metadataPath(volumeID)
	data, err := os.ReadFile(metadataPath)
	if err == nil {
		var metadata TunnelMetadata
		if err := json.Unmarshal(data, &metadata); err == nil {
			if validateErr := m.validateTunnelMetadata(&metadata); validateErr == nil {
				return &metadata, nil
			} else {
				m.logger.Warn("Primary metadata validation failed, attempting backup",
					zap.String("volumeID", volumeID),
					zap.String("metadataPath", metadataPath),
					zap.Error(validateErr))
			}
		} else {
			m.logger.Warn("Primary metadata unmarshal failed, attempting backup",
				zap.String("volumeID", volumeID),
				zap.String("metadataPath", metadataPath),
				zap.Error(err))
		}
	} else {
		m.logger.Warn("Primary metadata read failed, attempting backup",
			zap.String("volumeID", volumeID),
			zap.String("metadataPath", metadataPath),
			zap.Error(err))
	}

	backupPath := m.metadataBackupPath(volumeID)
	backupData, backupErr := os.ReadFile(backupPath)
	if backupErr != nil {
		return nil, fmt.Errorf("failed to load metadata from primary (%s) and backup (%s): primary error: %v, backup error: %w", metadataPath, backupPath, err, backupErr)
	}

	var backupMetadata TunnelMetadata
	if err := json.Unmarshal(backupData, &backupMetadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal backup metadata %s: %w", backupPath, err)
	}
	if err := m.validateTunnelMetadata(&backupMetadata); err != nil {
		return nil, fmt.Errorf("backup metadata validation failed for %s: %w", backupPath, err)
	}

	if writeErr := atomicWriteFile(metadataPath, backupData, 0600); writeErr != nil {
		m.logger.Warn("Failed to restore primary metadata from backup",
			zap.String("volumeID", volumeID),
			zap.String("metadataPath", metadataPath),
			zap.String("backupPath", backupPath),
			zap.Error(writeErr))
	} else {
		m.logger.Info("Restored primary metadata from backup",
			zap.String("volumeID", volumeID),
			zap.String("metadataPath", metadataPath),
			zap.String("backupPath", backupPath))
	}

	return &backupMetadata, nil
}

// deleteTunnelMetadata removes tunnel metadata from disk
func (m *Manager) deleteTunnelMetadata(volumeID string) {
	_ = os.Remove(m.metadataPath(volumeID))
	_ = os.Remove(m.metadataBackupPath(volumeID))
}

// RecoverFromCrash scans for persisted tunnel metadata and recreates tunnels
// This is called on startup to recover from driver crashes
func (m *Manager) RecoverFromCrash() error {
	m.logger.Info("Starting tunnel recovery from crash")

	// List all metadata files in config directory
	files, err := filepath.Glob(filepath.Join(m.configDir, "*.meta.json"))
	if err != nil {
		return fmt.Errorf("failed to list metadata files: %w", err)
	}

	recovered := 0
	failed := 0

	for _, metaFile := range files {
		// Extract volumeID from filename
		base := filepath.Base(metaFile)
		volumeID := strings.TrimSuffix(base, ".meta.json")

		// Load metadata
		metadata, err := m.loadTunnelMetadata(volumeID)
		if err != nil {
			m.logger.Warn("Failed to load tunnel metadata",
				zap.String("volumeID", volumeID),
				zap.Error(err))
			failed++
			continue
		}

		// Recreate tunnel on the original saved port so existing mounts keep working
		m.logger.Info("Recovering tunnel from metadata",
			zap.String("volumeID", volumeID),
			zap.String("nfsServer", metadata.NFSServer),
			zap.Int("port", metadata.Port),
			zap.Int("refCount", metadata.RefCount))

		tunnel, err := m.recoverTunnel(volumeID, metadata.NFSServer, metadata.Port, metadata.RefCount)
		if err != nil {
			m.logger.Error("Failed to recover tunnel",
				zap.String("volumeID", volumeID),
				zap.Int("port", metadata.Port),
				zap.Error(err))
			failed++
			continue
		}

		recovered++
		m.logger.Info("Recovered tunnel successfully",
			zap.String("volumeID", volumeID),
			zap.Int("port", tunnel.LocalPort),
			zap.Int("refCount", tunnel.RefCount))
	}

	m.logger.Info("Tunnel recovery completed",
		zap.Int("recovered", recovered),
		zap.Int("failed", failed))

	return nil
}

// allocatePort finds an available port for a volume
func (m *Manager) allocatePort(volumeID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Try hash-based allocation first for consistency
	h := fnv.New32a()
	h.Write([]byte(volumeID))
	preferredPort := m.basePort + int(h.Sum32()%uint32(m.portRange))

	// Check if preferred port is available
	if !m.allocatedPorts[preferredPort] && m.isPortAvailable(preferredPort) {
		m.allocatedPorts[preferredPort] = true
		return preferredPort, nil
	}

	// Find any available port in range
	for i := 0; i < m.portRange; i++ {
		port := m.basePort + i
		if !m.allocatedPorts[port] && m.isPortAvailable(port) {
			m.allocatedPorts[port] = true
			return port, nil
		}
	}

	return 0, fmt.Errorf("no available ports in range %d-%d", m.basePort, m.basePort+m.portRange-1)
}

// isPortAvailable checks if a port is available for binding
func (m *Manager) isPortAvailable(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

// releasePort marks a port as available
func (m *Manager) releasePort(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.allocatedPorts, port)
}

// reservePort reserves a specific port if it is available.
// Used during crash recovery so restored tunnels come back on the same local port.
func (m *Manager) reservePort(port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if port < m.basePort || port >= m.basePort+m.portRange {
		return fmt.Errorf("port %d is outside managed range %d-%d", port, m.basePort, m.basePort+m.portRange-1)
	}
	if m.allocatedPorts[port] {
		return fmt.Errorf("port %d is already allocated", port)
	}
	if !m.isPortAvailable(port) {
		return fmt.Errorf("port %d is not available", port)
	}

	m.allocatedPorts[port] = true
	return nil
}

// generateConfig creates a Stunnel configuration file with proper TLS verification
// The configuration is designed to work with host network and host filesystem access
func (m *Manager) generateConfig(volumeID, nfsServer string, port int) (string, error) {
	// Construct the checkHost based on environment
	checkHost := fmt.Sprintf("%s.is-share.appdomain.cloud", m.environment)

	config := fmt.Sprintf(`; Stunnel configuration for volume %s
; Generated at %s
; This configuration runs with host network access for NFS4 mounting

; Global options
client = yes
foreground = yes

; Service definition for NFS over TLS
[nfs-%s]
accept = 127.0.0.1:%d
connect = %s:%d
cafile = %s
checkHost = %s
verify = 1
`, volumeID, time.Now().Format(time.RFC3339), volumeID, port, nfsServer, m.nfsPort, m.caFile, checkHost)

	configPath := filepath.Join(m.configDir, fmt.Sprintf("%s.conf", volumeID))
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		return "", fmt.Errorf("failed to write config file: %w", err)
	}

	return configPath, nil
}

// EnsureTunnel ensures a tunnel exists for the given volume
// It increments the reference count if the tunnel already exists
func (m *Manager) EnsureTunnel(volumeID, nfsServer string) (*Tunnel, error) {
	m.mu.Lock()

	// Check if tunnel already exists and is healthy
	if t, ok := m.tunnels[volumeID]; ok {
		if t.State == StateRunning && m.checkTunnelHealth(t) {
			// Increment reference count for existing healthy tunnel
			t.RefCount++
			m.mu.Unlock()

			// Update metadata with new refcount
			if err := m.saveTunnelMetadataWithRetry(t); err != nil {
				m.logger.Warn("Failed to update tunnel metadata after retries",
					zap.String("volumeID", volumeID),
					zap.Error(err))
			}

			m.logger.Info("Tunnel already exists and is healthy, incremented refcount",
				zap.String("volumeID", volumeID),
				zap.Int("port", t.LocalPort),
				zap.Int("refCount", t.RefCount))
			return t, nil
		}

		// Tunnel exists but unhealthy, restart it
		m.logger.Warn("Existing tunnel is unhealthy, restarting",
			zap.String("volumeID", volumeID))
		m.mu.Unlock()

		if err := m.restartTunnel(t); err != nil {
			return nil, fmt.Errorf("failed to restart unhealthy tunnel: %w", err)
		}

		// Increment refcount after successful restart
		m.mu.Lock()
		t.RefCount++
		m.mu.Unlock()

		// Update metadata with new refcount
		if err := m.saveTunnelMetadataWithRetry(t); err != nil {
			m.logger.Warn("Failed to update tunnel metadata after retries",
				zap.String("volumeID", volumeID),
				zap.Error(err))
		}

		return t, nil
	}
	m.mu.Unlock()

	// Allocate port
	port, err := m.allocatePort(volumeID)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate port: %w", err)
	}

	return m.createTunnel(volumeID, nfsServer, port, 1, true)
}

// recoverTunnel recreates a tunnel on its original saved port for crash recovery.
func (m *Manager) recoverTunnel(volumeID, nfsServer string, port, refCount int) (*Tunnel, error) {
	if err := m.reservePort(port); err != nil {
		return nil, fmt.Errorf("failed to reserve recovered port: %w", err)
	}
	return m.createTunnel(volumeID, nfsServer, port, refCount, false)
}

// createTunnel creates and starts a tunnel on a specified port.
// saveMetadata controls whether metadata should be rewritten after creation.
func (m *Manager) createTunnel(volumeID, nfsServer string, port, refCount int, saveMetadata bool) (*Tunnel, error) {
	m.logger.Info("Preparing tunnel",
		zap.String("volumeID", volumeID),
		zap.String("nfsServer", nfsServer),
		zap.Int("port", port),
		zap.Int("refCount", refCount))

	// Generate configuration
	configPath, err := m.generateConfig(volumeID, nfsServer, port)
	if err != nil {
		m.releasePort(port)
		return nil, fmt.Errorf("failed to generate config: %w", err)
	}

	// Crude way: Directly read and print file contents
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		m.releasePort(port)
		_ = os.Remove(configPath)
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	m.logger.Info("configContent", zap.String("configContent", string(configContent)))

	// Create tunnel context
	ctx, cancel := context.WithCancel(context.Background())

	tunnel := &Tunnel{
		VolumeID:   volumeID,
		RemoteAddr: nfsServer,
		LocalPort:  port,
		ConfigPath: configPath,
		State:      StateStarting,
		RefCount:   refCount,
		ctx:        ctx,
		cancel:     cancel,
		logger:     m.logger.With(zap.String("volumeID", volumeID)),
	}

	// Start Stunnel process
	if err := m.startTunnelProcess(tunnel); err != nil {
		cancel()
		m.releasePort(port)
		_ = os.Remove(configPath)
		return nil, fmt.Errorf("failed to start tunnel process: %w", err)
	}

	// Wait for tunnel to be ready
	if err := m.waitForTunnel(tunnel, 10*time.Second); err != nil {
		cancel()
		m.stopTunnelProcess(tunnel)
		m.releasePort(port)
		_ = os.Remove(configPath)
		return nil, fmt.Errorf("tunnel failed to become ready: %w", err)
	}

	// Register tunnel
	m.mu.Lock()
	m.tunnels[volumeID] = tunnel
	m.mu.Unlock()

	tunnel.State = StateRunning
	tunnel.LastHealthy = time.Now()

	if saveMetadata {
		if err := m.saveTunnelMetadata(tunnel); err != nil {
			m.logger.Warn("Failed to save tunnel metadata",
				zap.String("volumeID", volumeID),
				zap.Error(err))
			// Don't fail tunnel creation if metadata save fails
		}
	}

	m.logger.Info("Tunnel created successfully",
		zap.String("volumeID", volumeID),
		zap.String("nfsServer", nfsServer),
		zap.Int("port", port),
		zap.Int("refCount", refCount))

	return tunnel, nil
}

// startTunnelProcess starts the Stunnel process for a tunnel
func (m *Manager) startTunnelProcess(t *Tunnel) error {
	cmd := exec.CommandContext(t.ctx, "stunnel", t.ConfigPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start stunnel: %w", err)
	}

	t.Cmd = cmd
	t.logger.Info("Stunnel process started", zap.Int("pid", cmd.Process.Pid))

	// Monitor process in background
	go m.monitorProcess(t)

	return nil
}

// monitorProcess monitors a tunnel process and handles crashes
func (m *Manager) monitorProcess(t *Tunnel) {
	err := t.Cmd.Wait()

	// Check if context was cancelled (intentional stop)
	select {
	case <-t.ctx.Done():
		t.logger.Info("Tunnel process stopped intentionally")
		return
	default:
	}

	// Process crashed unexpectedly
	t.logger.Error("Tunnel process crashed", zap.Error(err))
	t.State = StateFailed
	t.RestartCount++

	// Attempt restart with backoff (max 3 attempts)
	if t.RestartCount <= 3 {
		backoff := time.Duration(t.RestartCount) * 2 * time.Second
		t.logger.Info("Attempting to restart tunnel",
			zap.Int("restartCount", t.RestartCount),
			zap.Duration("backoff", backoff))

		time.Sleep(backoff)

		if err := m.restartTunnel(t); err != nil {
			t.logger.Error("Failed to restart tunnel", zap.Error(err))
		}
	} else {
		t.logger.Error("Tunnel restart limit exceeded")
	}
}

// restartTunnel restarts a failed tunnel
func (m *Manager) restartTunnel(t *Tunnel) error {
	// Stop existing process if running
	m.stopTunnelProcess(t)

	// Create new context
	ctx, cancel := context.WithCancel(context.Background())
	t.ctx = ctx
	t.cancel = cancel
	t.State = StateStarting

	// Start new process
	if err := m.startTunnelProcess(t); err != nil {
		cancel()
		return err
	}

	// Wait for tunnel to be ready
	if err := m.waitForTunnel(t, 10*time.Second); err != nil {
		cancel()
		m.stopTunnelProcess(t)
		return err
	}

	t.State = StateRunning
	t.LastHealthy = time.Now()
	t.logger.Info("Tunnel restarted successfully")

	return nil
}

// stopTunnelProcess stops a tunnel process
func (m *Manager) stopTunnelProcess(t *Tunnel) {
	if t.Cmd != nil && t.Cmd.Process != nil {
		t.Cmd.Process.Kill()
		t.Cmd.Process.Wait()
	}
}

// waitForTunnel waits for a tunnel to become ready
func (m *Manager) waitForTunnel(t *Tunnel, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", t.LocalPort)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("tunnel did not become ready within %v", timeout)
}

// checkTunnelHealth checks if a tunnel is healthy
func (m *Manager) checkTunnelHealth(t *Tunnel) bool {
	if t.State != StateRunning {
		return false
	}

	// Check if process is still running
	if t.Cmd == nil || t.Cmd.Process == nil {
		return false
	}

	// Check if port is still listening
	addr := fmt.Sprintf("127.0.0.1:%d", t.LocalPort)
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return false
	}
	conn.Close()

	return true
}

// healthCheckLoop periodically checks tunnel health
func (m *Manager) healthCheckLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(m.healthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.performHealthChecks()
		case <-m.healthStop:
			return
		}
	}
}

// performHealthChecks checks health of all tunnels
func (m *Manager) performHealthChecks() {
	m.mu.RLock()
	tunnels := make([]*Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		tunnels = append(tunnels, t)
	}
	m.mu.RUnlock()

	for _, t := range tunnels {
		if m.checkTunnelHealth(t) {
			t.LastHealthy = time.Now()
		} else if t.State == StateRunning {
			t.logger.Warn("Tunnel health check failed, attempting restart")
			if err := m.restartTunnel(t); err != nil {
				t.logger.Error("Failed to restart unhealthy tunnel", zap.Error(err))
			}
		}
	}
}

// RemoveTunnel decrements the reference count and removes the tunnel only when refcount reaches 0
func (m *Manager) RemoveTunnel(volumeID string) error {
	m.mu.Lock()
	t, ok := m.tunnels[volumeID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("tunnel not found for volume %s", volumeID)
	}

	// Decrement reference count
	t.RefCount--

	m.logger.Info("Decremented tunnel refcount",
		zap.String("volumeID", volumeID),
		zap.Int("refCount", t.RefCount))

	// Only remove tunnel if no more references
	if t.RefCount > 0 {
		m.mu.Unlock()

		// Update metadata with new refcount
		if err := m.saveTunnelMetadata(t); err != nil {
			m.logger.Warn("Failed to update tunnel metadata",
				zap.String("volumeID", volumeID),
				zap.Error(err))
		}

		m.logger.Info("Tunnel still in use, keeping it active",
			zap.String("volumeID", volumeID),
			zap.Int("refCount", t.RefCount))
		return nil
	}

	// RefCount is 0, remove the tunnel
	delete(m.tunnels, volumeID)
	m.mu.Unlock()

	m.logger.Info("Removing tunnel (refcount reached 0)", zap.String("volumeID", volumeID))

	// Cancel context to stop monitoring
	t.cancel()

	// Stop process
	m.stopTunnelProcess(t)

	// Release port
	m.releasePort(t.LocalPort)

	// Clean up config file
	if t.ConfigPath != "" {
		os.Remove(t.ConfigPath)
	}

	// Delete tunnel metadata
	m.deleteTunnelMetadata(volumeID)

	t.State = StateStopped
	m.logger.Info("Tunnel removed successfully", zap.String("volumeID", volumeID))

	return nil
}

// GetTunnel returns a tunnel by volume ID
func (m *Manager) GetTunnel(volumeID string) (*Tunnel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tunnels[volumeID]
	return t, ok
}

// GetLocalEndpoint returns the local endpoint for a volume
func (m *Manager) GetLocalEndpoint(volumeID string) (string, error) {
	t, ok := m.GetTunnel(volumeID)
	if !ok {
		return "", fmt.Errorf("tunnel not found for volume %s", volumeID)
	}
	return fmt.Sprintf("127.0.0.1:%d", t.LocalPort), nil
}

// ToTunnelInfo converts an in-memory tunnel to a transport-safe DTO
func ToTunnelInfo(t *Tunnel) *TunnelInfo {
	if t == nil {
		return nil
	}
	return &TunnelInfo{
		VolumeID:   t.VolumeID,
		RemoteAddr: t.RemoteAddr,
		LocalPort:  t.LocalPort,
		State:      string(t.State),
		RefCount:   t.RefCount,
	}
}

// Shutdown gracefully shuts down the manager
func (m *Manager) Shutdown() error {
	m.logger.Info("Shutting down tunnel manager")

	// Stop health check loop
	close(m.healthStop)
	m.wg.Wait()

	// Remove all tunnels
	m.mu.Lock()
	volumeIDs := make([]string, 0, len(m.tunnels))
	for volumeID := range m.tunnels {
		volumeIDs = append(volumeIDs, volumeID)
	}
	m.mu.Unlock()

	for _, volumeID := range volumeIDs {
		if err := m.RemoveTunnel(volumeID); err != nil {
			m.logger.Error("Failed to remove tunnel during shutdown",
				zap.String("volumeID", volumeID),
				zap.Error(err))
		}
	}

	m.logger.Info("Tunnel manager shutdown complete")
	return nil
}

// GetConfigFromEnv creates a Config from environment variables
func GetConfigFromEnv(logger *zap.Logger) *Config {
	cfg := &Config{
		Logger: logger,
	}

	if basePort := os.Getenv("STUNNEL_BASE_PORT"); basePort != "" {
		if port, err := strconv.Atoi(basePort); err == nil {
			cfg.BasePort = port
		}
	}

	if portRange := os.Getenv("STUNNEL_PORT_RANGE"); portRange != "" {
		if pr, err := strconv.Atoi(portRange); err == nil {
			cfg.PortRange = pr
		}
	}

	if configDir := os.Getenv("STUNNEL_CONFIG_DIR"); configDir != "" {
		cfg.ConfigDir = configDir
	}

	if caFile := os.Getenv("STUNNEL_CA_FILE"); caFile != "" {
		cfg.CAFile = caFile
	}

	if nfsPort := os.Getenv("STUNNEL_NFS_PORT"); nfsPort != "" {
		if port, err := strconv.Atoi(nfsPort); err == nil {
			cfg.NFSPort = port
		}
	}

	if env := os.Getenv("STUNNEL_ENVIRONMENT"); env != "" {
		cfg.Environment = env
	}

	if healthInterval := os.Getenv("STUNNEL_HEALTH_CHECK_INTERVAL"); healthInterval != "" {
		if duration, err := time.ParseDuration(healthInterval); err == nil {
			cfg.HealthInterval = duration
		}
	}

	return cfg
}
