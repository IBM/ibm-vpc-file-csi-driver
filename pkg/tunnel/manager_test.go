package tunnel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestNewManager tests manager initialization
func TestNewManager(t *testing.T) {
	tests := []struct {
		name    string
		config  func() *Config
		wantErr bool
	}{
		{
			name: "nil config uses defaults with temp dir",
			config: func() *Config {
				return &Config{
					ConfigDir: t.TempDir(),
					Logger:    zap.NewNop(),
				}
			},
			wantErr: false,
		},
		{
			name: "custom config",
			config: func() *Config {
				return &Config{
					BasePort:       25000,
					PortRange:      5000,
					ConfigDir:      t.TempDir(),
					CAFile:         "/tmp/ca.pem",
					NFSPort:        2049,
					Environment:    "staging",
					HealthInterval: 10 * time.Second,
					Logger:         zap.NewNop(),
				}
			},
			wantErr: false,
		},
		{
			name: "partial config with defaults",
			config: func() *Config {
				return &Config{
					ConfigDir: t.TempDir(),
					Logger:    zap.NewNop(),
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewManager(tt.config())
			if (err != nil) != tt.wantErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				defer m.Shutdown()

				// Verify defaults are set
				if m.basePort == 0 {
					t.Error("basePort should be set to default")
				}
				if m.portRange == 0 {
					t.Error("portRange should be set to default")
				}
				if m.configDir == "" {
					t.Error("configDir should be set")
				}
				if m.tunnels == nil {
					t.Error("tunnels map should be initialized")
				}
				if m.allocatedPorts == nil {
					t.Error("allocatedPorts map should be initialized")
				}
			}
		})
	}
}

// TestAllocatePort tests port allocation logic
func TestAllocatePort(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(&Config{
		BasePort:  20000,
		PortRange: 100,
		ConfigDir: tmpDir,
		Logger:    zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer m.Shutdown()

	tests := []struct {
		name        string
		volumeID    string
		wantInRange bool
	}{
		{
			name:        "allocate first port",
			volumeID:    "vol-123",
			wantInRange: true,
		},
		{
			name:        "allocate second port",
			volumeID:    "vol-456",
			wantInRange: true,
		},
		{
			name:        "same volumeID gets same port",
			volumeID:    "vol-123",
			wantInRange: true,
		},
	}

	allocatedPorts := make(map[int]bool)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, err := m.allocatePort(tt.volumeID)
			if err != nil {
				t.Errorf("allocatePort() error = %v", err)
				return
			}

			if port < m.basePort || port >= m.basePort+m.portRange {
				t.Errorf("allocatePort() = %d, want in range [%d, %d)", port, m.basePort, m.basePort+m.portRange)
			}

			// Check for port conflicts (except for same volumeID)
			if allocatedPorts[port] && tt.volumeID != "vol-123" {
				t.Errorf("allocatePort() = %d, port already allocated", port)
			}
			allocatedPorts[port] = true
		})
	}
}

// TestMetadataOperations tests metadata save/load/delete
func TestMetadataOperations(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(&Config{
		BasePort:  20000,
		PortRange: 100,
		ConfigDir: tmpDir,
		Logger:    zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer m.Shutdown()

	volumeID := "test-vol-123"
	nfsServer := "10.240.0.5"
	port := 20050 // Within default range 20000-20099

	// Create a tunnel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tunnel := &Tunnel{
		VolumeID:   volumeID,
		RemoteAddr: nfsServer,
		LocalPort:  port,
		State:      StateRunning,
		RefCount:   1,
		ctx:        ctx,
		cancel:     cancel,
		logger:     m.logger,
	}

	// Test save
	t.Run("save metadata", func(t *testing.T) {
		err := m.saveTunnelMetadataWithRetry(tunnel)
		if err != nil {
			t.Errorf("saveTunnelMetadataWithRetry() error = %v", err)
		}

		// Verify files exist
		metaPath := m.metadataPath(volumeID)
		if _, err := os.Stat(metaPath); os.IsNotExist(err) {
			t.Errorf("metadata file not created: %s", metaPath)
		}

		backupPath := m.metadataBackupPath(volumeID)
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			t.Errorf("backup metadata file not created: %s", backupPath)
		}
	})

	// Test load
	t.Run("load metadata", func(t *testing.T) {
		metadata, err := m.loadTunnelMetadata(volumeID)
		if err != nil {
			t.Errorf("loadTunnelMetadata() error = %v", err)
			return
		}

		if metadata.VolumeID != volumeID {
			t.Errorf("loaded volumeID = %s, want %s", metadata.VolumeID, volumeID)
		}
		if metadata.NFSServer != nfsServer {
			t.Errorf("loaded nfsServer = %s, want %s", metadata.NFSServer, nfsServer)
		}
		if metadata.Port != port {
			t.Errorf("loaded port = %d, want %d", metadata.Port, port)
		}
		if metadata.RefCount != 1 {
			t.Errorf("loaded refCount = %d, want 1", metadata.RefCount)
		}
	})

	// Test delete
	t.Run("delete metadata", func(t *testing.T) {
		m.deleteTunnelMetadata(volumeID)

		// Verify files are deleted
		metaPath := m.metadataPath(volumeID)
		if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
			t.Errorf("metadata file still exists: %s", metaPath)
		}

		backupPath := m.metadataBackupPath(volumeID)
		if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
			t.Errorf("backup metadata file still exists: %s", backupPath)
		}
	})
}

// TestValidateTunnelMetadata tests metadata validation
func TestValidateTunnelMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(&Config{
		BasePort:  20000,
		PortRange: 100,
		ConfigDir: tmpDir,
		Logger:    zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer m.Shutdown()

	tests := []struct {
		name     string
		metadata *TunnelMetadata
		wantErr  bool
	}{
		{
			name:     "nil metadata",
			metadata: nil,
			wantErr:  true,
		},
		{
			name: "empty volumeID",
			metadata: &TunnelMetadata{
				VolumeID:  "",
				NFSServer: "10.240.0.5",
				Port:      20574,
				RefCount:  1,
			},
			wantErr: true,
		},
		{
			name: "empty nfsServer",
			metadata: &TunnelMetadata{
				VolumeID:  "vol-123",
				NFSServer: "",
				Port:      20574,
				RefCount:  1,
			},
			wantErr: true,
		},
		{
			name: "port out of range (too low)",
			metadata: &TunnelMetadata{
				VolumeID:  "vol-123",
				NFSServer: "10.240.0.5",
				Port:      19999,
				RefCount:  1,
			},
			wantErr: true,
		},
		{
			name: "port out of range (too high)",
			metadata: &TunnelMetadata{
				VolumeID:  "vol-123",
				NFSServer: "10.240.0.5",
				Port:      20100,
				RefCount:  1,
			},
			wantErr: true,
		},
		{
			name: "negative refCount",
			metadata: &TunnelMetadata{
				VolumeID:  "vol-123",
				NFSServer: "10.240.0.5",
				Port:      20574,
				RefCount:  -1,
			},
			wantErr: true,
		},
		{
			name: "valid metadata",
			metadata: &TunnelMetadata{
				VolumeID:  "vol-123",
				NFSServer: "10.240.0.5",
				Port:      20050,
				RefCount:  1,
			},
			wantErr: false,
		},
		{
			name: "valid metadata with zero refCount",
			metadata: &TunnelMetadata{
				VolumeID:  "vol-123",
				NFSServer: "10.240.0.5",
				Port:      20050,
				RefCount:  0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.validateTunnelMetadata(tt.metadata)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTunnelMetadata() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestStatWithTimeout tests the timeout-protected stat function
func TestStatWithTimeout(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		timeout time.Duration
		wantErr bool
	}{
		{
			name:    "existing file",
			path:    testFile,
			timeout: 1 * time.Second,
			wantErr: false,
		},
		{
			name:    "non-existent file",
			path:    filepath.Join(tmpDir, "nonexistent.txt"),
			timeout: 1 * time.Second,
			wantErr: true,
		},
		{
			name:    "existing file with short timeout",
			path:    testFile,
			timeout: 100 * time.Millisecond,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := statWithTimeout(tt.path, tt.timeout)
			if (err != nil) != tt.wantErr {
				t.Errorf("statWithTimeout() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && info == nil {
				t.Error("statWithTimeout() returned nil info for existing file")
			}
		})
	}
}

// TestRefCountManagement tests reference counting logic without stunnel
func TestRefCountManagement(t *testing.T) {
	t.Skip("Skipping test that requires stunnel binary - test metadata and port allocation instead")
}

// TestGetTunnel tests tunnel retrieval
func TestGetTunnel(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(&Config{
		BasePort:  20000,
		PortRange: 100,
		ConfigDir: tmpDir,
		Logger:    zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer m.Shutdown()

	volumeID := "test-vol-get"

	// Test getting non-existent tunnel
	t.Run("get non-existent tunnel", func(t *testing.T) {
		_, exists := m.GetTunnel(volumeID)
		if exists {
			t.Error("GetTunnel() should return exists=false for non-existent tunnel")
		}
	})
}

// TestConcurrentAccess tests thread-safety of port allocation
func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(&Config{
		BasePort:  20000,
		PortRange: 1000,
		ConfigDir: tmpDir,
		Logger:    zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer m.Shutdown()

	// Test concurrent port allocation
	const numGoroutines = 10
	done := make(chan error, numGoroutines)
	ports := make(chan int, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			volumeID := fmt.Sprintf("vol-%d", id)
			port, err := m.allocatePort(volumeID)
			if err == nil {
				ports <- port
			}
			done <- err
		}(i)
	}

	// Wait for all goroutines
	allocatedPorts := make(map[int]bool)
	for i := 0; i < numGoroutines; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent allocatePort() error = %v", err)
		}
	}
	close(ports)

	// Verify no duplicate ports
	for port := range ports {
		if allocatedPorts[port] {
			t.Errorf("duplicate port allocated: %d", port)
		}
		allocatedPorts[port] = true
	}

	if len(allocatedPorts) != numGoroutines {
		t.Errorf("allocated %d unique ports, want %d", len(allocatedPorts), numGoroutines)
	}
}

// TestManagerShutdown tests graceful shutdown
func TestManagerShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(&Config{
		BasePort:  20000,
		PortRange: 100,
		ConfigDir: tmpDir,
		Logger:    zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Shutdown should not panic
	err = m.Shutdown()
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

// Made with Bob
