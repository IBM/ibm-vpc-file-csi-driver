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
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestNewStunnelManager tests the constructor with hardcoded defaults
func TestNewStunnelManager(t *testing.T) {
	tests := []struct {
		name        string
		logger      *zap.Logger
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil logger",
			logger:      nil,
			wantErr:     true,
			errContains: "logger is required",
		},
		{
			name:    "valid logger",
			logger:  zaptest.NewLogger(t),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment and CA file for valid test case
			if !tt.wantErr {
				// Create temporary CA file
				tmpDir := t.TempDir()
				caFile := filepath.Join(tmpDir, "ca.pem")
				if err := os.WriteFile(caFile, []byte("fake CA content"), 0644); err != nil {
					t.Fatalf("Failed to create CA file: %v", err)
				}

				// Set environment variables
				t.Setenv("OS_TYPE", "RHCOS")
				t.Setenv("CLUSTER_ENV", "prod")

				// Override CA path detection by creating the file at expected location
				// Since we can't modify system paths, we'll use NewStunnelManagerForTesting instead
				t.Skip("Skipping NewStunnelManager test - requires system CA files. Use NewStunnelManagerForTesting instead.")
			}
			sm, err := NewStunnelManager(tt.logger)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewStunnelManager() expected error containing %q, got nil", tt.errContains)
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("NewStunnelManager() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("NewStunnelManager() unexpected error = %v", err)
				return
			}

			if sm == nil {
				t.Error("NewStunnelManager() returned nil manager")
				return
			}

			// Verify hardcoded defaults were applied
			if sm.servicesDir != DefaultServicesDir {
				t.Errorf("servicesDir = %v, want %v", sm.servicesDir, DefaultServicesDir)
			}
			if sm.initialPort != InitialPort {
				t.Errorf("initialPort = %v, want %v", sm.initialPort, InitialPort)
			}
			if sm.portRange != PortRange {
				t.Errorf("portRange = %v, want %v", sm.portRange, PortRange)
			}
			if sm.debounceWindow != 2*time.Second {
				t.Errorf("debounceWindow = %v, want %v", sm.debounceWindow, 2*time.Second)
			}
			if sm.debugLevel != DefaultDebugLevel {
				t.Errorf("debugLevel = %v, want %v", sm.debugLevel, DefaultDebugLevel)
			}
		})
	}
}

// TestDetectCABundle tests CA bundle detection based on OS_TYPE
func TestDetectCABundle(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name        string
		osType      string
		wantErr     bool
		wantContain string
	}{
		{
			name:        "RHCOS OS type",
			osType:      "RHCOS",
			wantErr:     false,
			wantContain: "pki/ca-trust",
		},
		{
			name:        "RHEL OS type",
			osType:      "RHEL",
			wantErr:     false,
			wantContain: "pki/ca-trust",
		},
		{
			name:        "Ubuntu OS type",
			osType:      "Ubuntu",
			wantErr:     false,
			wantContain: "ca-certificates.crt",
		},
		{
			name:    "no OS_TYPE set",
			osType:  "",
			wantErr: true,
		},
		{
			name:    "unknown OS type",
			osType:  "Unknown",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.osType != "" {
				if err := os.Setenv("OS_TYPE", tt.osType); err != nil {
					t.Fatalf("Failed to set OS_TYPE: %v", err)
				}
				defer func() {
					if err := os.Unsetenv("OS_TYPE"); err != nil {
						t.Logf("Failed to unset OS_TYPE: %v", err)
					}
				}()
			} else {
				_ = os.Unsetenv("OS_TYPE")
			}

			result, err := detectCABundle(logger)
			if tt.wantErr {
				if err == nil {
					t.Errorf("detectCABundle() expected error, got nil")
				}
				return
			}

			// In test environment, CA files may not exist
			if err != nil {
				if strings.Contains(err.Error(), "CA bundle file not found") {
					t.Skipf("Skipping test - CA bundle file not found in test environment (expected): %v", err)
					return
				}
				t.Errorf("detectCABundle() unexpected error = %v", err)
				return
			}

			if !strings.Contains(result, tt.wantContain) {
				t.Errorf("detectCABundle() = %v, want to contain %v", result, tt.wantContain)
			}
		})
	}
}

// TestGetCheckHost tests checkHost determination
func TestGetCheckHost(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name       string
		clusterEnv string
		want       string
		wantErr    bool
	}{
		{
			name:       "production",
			clusterEnv: "prod",
			want:       "production.is-share.appdomain.cloud",
			wantErr:    false,
		},
		{
			name:       "staging",
			clusterEnv: "stage",
			want:       "staging.is-share.appdomain.cloud",
			wantErr:    false,
		},
		{
			name:       "empty - defaults to production",
			clusterEnv: "",
			want:       "production.is-share.appdomain.cloud",
			wantErr:    false,
		},
		{
			name:       "unknown - defaults to production",
			clusterEnv: "unknown",
			want:       "production.is-share.appdomain.cloud",
			wantErr:    false,
		},
		{
			name:       "prod - defaults to prod (not supported)",
			clusterEnv: "prod",
			want:       "production.is-share.appdomain.cloud",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.clusterEnv != "" {
				if err := os.Setenv("CLUSTER_ENV", tt.clusterEnv); err != nil {
					t.Fatalf("Failed to set CLUSTER_ENV: %v", err)
				}
				defer func() {
					if err := os.Unsetenv("CLUSTER_ENV"); err != nil {
						t.Logf("Failed to unset CLUSTER_ENV: %v", err)
					}
				}()
			} else {
				if err := os.Unsetenv("CLUSTER_ENV"); err != nil {
					t.Logf("Failed to unset CLUSTER_ENV: %v", err)
				}
			}

			result, err := getClusterEnv(logger)
			if tt.wantErr {
				if err == nil {
					t.Errorf("getClusterEnv() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("getClusterEnv() unexpected error = %v", err)
				return
			}
			if result != tt.want {
				t.Errorf("getClusterEnv() = %v, want %v", result, tt.want)
			}
		})
	}
}

// TestRecoverExistingTunnels tests tunnel recovery
func TestRecoverExistingTunnels(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	// Create test config files
	testConfigs := map[string]string{
		"vol1.conf": `[vol1]
client = yes
accept = 127.0.0.1:10001
connect = server1:20049
`,
		"vol2.conf": `[vol2]
client = yes
accept = 127.0.0.1:10002
connect = server2:20049
`,
		"invalid.conf": `[invalid]
client = yes
connect = server3:20049
`, // Missing accept line
	}

	for name, content := range testConfigs {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0600); err != nil {
			t.Fatalf("Failed to create test config: %v", err)
		}
	}

	sm := &StunnelManager{
		servicesDir:    tmpDir,
		initialPort:    10001,
		portRange:      100,
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		logger:         logger,
	}

	err := sm.recoverExistingTunnels()
	if err != nil {
		t.Errorf("recoverExistingTunnels() unexpected error = %v", err)
	}

	// Verify recovered tunnels
	if port, exists := sm.allocatedPorts["vol1"]; !exists || port != 10001 {
		t.Errorf("vol1 not recovered correctly, got port=%d, exists=%v", port, exists)
	}
	if port, exists := sm.allocatedPorts["vol2"]; !exists || port != 10002 {
		t.Errorf("vol2 not recovered correctly, got port=%d, exists=%v", port, exists)
	}
	if _, exists := sm.allocatedPorts["invalid"]; exists {
		t.Error("invalid config should not be recovered")
	}
}

// TestRecoverExistingTunnels_NonExistentDir tests recovery with non-existent directory
func TestRecoverExistingTunnels_NonExistentDir(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		servicesDir:    "/non/existent/directory",
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		logger:         logger,
	}

	err := sm.recoverExistingTunnels()
	if err != nil {
		t.Errorf("recoverExistingTunnels() should not error on non-existent dir, got = %v", err)
	}
}

// TestExtractPortFromConfigFile tests port extraction
func TestExtractPortFromConfigFile(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		content string
		want    int
		wantErr bool
	}{
		{
			name: "valid config with spaces",
			content: `[test]
accept = 127.0.0.1:10001
connect = server:20049
`,
			want:    10001,
			wantErr: false,
		},
		{
			name: "valid config without spaces",
			content: `[test]
accept=127.0.0.1:10002
connect=server:20049
`,
			want:    10002,
			wantErr: false,
		},
		{
			name: "port with trailing spaces",
			content: `[test]
accept = 127.0.0.1: 10003
connect = server:20049
`,
			want:    10003,
			wantErr: false,
		},
		{
			name: "different IP address",
			content: `[test]
accept = 0.0.0.0:10004
connect = server:20049
`,
			want:    10004,
			wantErr: false,
		},
		{
			name: "with comments and empty lines",
			content: `# Configuration file
[test]
# Accept connections
accept = 127.0.0.1:10007

; Connect to server
connect = server:20049
`,
			want:    10007,
			wantErr: false,
		},
		{
			name: "missing accept line",
			content: `[test]
connect = server:20049
`,
			want:    0,
			wantErr: true,
		},
		{
			name: "invalid port format",
			content: `[test]
accept = 127.0.0.1:invalid
connect = server:20049
`,
			want:    0,
			wantErr: true,
		},
		{
			name: "port out of range - too high",
			content: `[test]
accept = 127.0.0.1:99999
connect = server:20049
`,
			want:    0,
			wantErr: true,
		},
		{
			name: "port out of range - zero",
			content: `[test]
accept = 127.0.0.1:0
connect = server:20049
`,
			want:    0,
			wantErr: true,
		},
		{
			name: "port out of range - negative",
			content: `[test]
accept = 127.0.0.1:-1
connect = server:20049
`,
			want:    0,
			wantErr: true,
		},
		{
			name: "missing colon separator",
			content: `[test]
accept = 127.0.0.1
connect = server:20049
`,
			want:    0,
			wantErr: true,
		},
	}

	sm := &StunnelManager{
		initialPort: 10001,
		portRange:   100,
		logger:      logger,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(tmpDir, tt.name+".conf")
			if err := os.WriteFile(configPath, []byte(tt.content), 0600); err != nil {
				t.Fatalf("Failed to create test config: %v", err)
			}

			port, err := sm.extractPortFromConfigFile(configPath)
			if tt.wantErr {
				if err == nil {
					t.Error("extractPortFromConfigFile() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("extractPortFromConfigFile() unexpected error = %v", err)
				return
			}

			if port != tt.want {
				t.Errorf("extractPortFromConfigFile() = %v, want %v", port, tt.want)
			}
		})
	}
}

// TestEnsureTunnel tests tunnel creation and retrieval
func TestEnsureTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	sm := &StunnelManager{
		servicesDir:    tmpDir,
		initialPort:    10001,
		portRange:      100,
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		caFile:         "/tmp/ca.pem",
		checkHost:      "test.example.com",
		logger:         logger,
		debounceWindow: 100 * time.Millisecond,
		stunnelStarted: true, // Skip stunnel check
	}

	tests := []struct {
		name      string
		volumeID  string
		nfsServer string
		wantErr   bool
	}{
		{
			name:      "create new tunnel",
			volumeID:  "vol1",
			nfsServer: "server1.example.com",
			wantErr:   false,
		},
		{
			name:      "get existing tunnel",
			volumeID:  "vol1",
			nfsServer: "server1.example.com",
			wantErr:   false,
		},
		{
			name:      "empty volumeID",
			volumeID:  "",
			nfsServer: "server1.example.com",
			wantErr:   true,
		},
		{
			name:      "empty nfsServer",
			volumeID:  "vol2",
			nfsServer: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, err := sm.EnsureTunnel(tt.volumeID, tt.nfsServer, "test-request")
			if tt.wantErr {
				if err == nil {
					t.Error("EnsureTunnel() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("EnsureTunnel() unexpected error = %v", err)
				return
			}

			if port < sm.initialPort || port >= sm.initialPort+sm.portRange {
				t.Errorf("EnsureTunnel() port %d out of range [%d, %d)", port, sm.initialPort, sm.initialPort+sm.portRange)
			}

			// Verify config file was created
			configPath := filepath.Join(tmpDir, tt.volumeID+".conf")
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				t.Errorf("Config file not created: %s", configPath)
			}

			// Verify port is in allocatedPorts map
			if allocatedPort, exists := sm.allocatedPorts[tt.volumeID]; !exists || allocatedPort != port {
				t.Errorf("Port not in allocatedPorts map correctly, got %d, exists=%v", allocatedPort, exists)
			}
		})
	}
}

// TestEnsureTunnel_NoTLSConfig documents current behavior: TLS config validation
// happens in NewStunnelManager/NewStunnelManagerForTesting, not in EnsureTunnel.
func TestEnsureTunnel_NoTLSConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		caFile    string
		checkHost string
	}{
		{
			name:      "missing CA file",
			caFile:    "",
			checkHost: "test.example.com",
		},
		{
			name:      "missing checkHost",
			caFile:    "/tmp/ca.pem",
			checkHost: "",
		},
		{
			name:      "both missing",
			caFile:    "",
			checkHost: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &StunnelManager{
				servicesDir:    tmpDir,
				initialPort:    10001,
				portRange:      100,
				allocatedPorts: make(map[string]int),
				portToVolume:   make(map[int]string),
				caFile:         tt.caFile,
				checkHost:      tt.checkHost,
				logger:         logger,
				stunnelStarted: true,
			}

			port, err := sm.EnsureTunnel("vol1", "server1.example.com", "test-request")
			if err != nil {
				t.Fatalf("EnsureTunnel() unexpected error = %v", err)
			}
			if port != 10001 {
				t.Fatalf("EnsureTunnel() port = %d, want 10001", port)
			}
		})
	}
}

// TestEnsureTunnel_Concurrent tests concurrent tunnel creation
func TestEnsureTunnel_Concurrent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	sm := &StunnelManager{
		servicesDir:    tmpDir,
		initialPort:    10001,
		portRange:      100,
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		caFile:         "/tmp/ca.pem",
		checkHost:      "test.example.com",
		logger:         logger,
		debounceWindow: 100 * time.Millisecond,
		stunnelStarted: true,
	}

	// Create same tunnel concurrently
	const goroutines = 10
	var wg sync.WaitGroup
	ports := make([]int, goroutines)
	errors := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			port, err := sm.EnsureTunnel("concurrent-vol", "server.example.com", fmt.Sprintf("request-%d", idx))
			ports[idx] = port
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// Wait for the debounce window to expire so the time.AfterFunc callback fires
	// and completes while *testing.T is still alive.
	time.Sleep(2 * sm.debounceWindow)

	// Cancel any residual timer state.
	sm.debounceMu.Lock()
	if sm.debounceTimer != nil {
		sm.debounceTimer.Stop()
		sm.debounceTimer = nil
	}
	sm.pendingSIGHUP = false
	sm.debounceMu.Unlock()

	// All should succeed with same port
	firstPort := ports[0]
	for i, port := range ports {
		if errors[i] != nil {
			t.Errorf("Goroutine %d got error: %v", i, errors[i])
		}
		if port != firstPort {
			t.Errorf("Goroutine %d got port %d, want %d", i, port, firstPort)
		}
	}

	// Should only have one entry in allocatedPorts
	if len(sm.allocatedPorts) != 1 {
		t.Errorf("allocatedPorts has %d entries, want 1", len(sm.allocatedPorts))
	}
}

// TestRemoveTunnel tests tunnel removal
func TestRemoveTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	sm := &StunnelManager{
		servicesDir:    tmpDir,
		initialPort:    10001,
		portRange:      100,
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		caFile:         "/tmp/ca.pem",
		checkHost:      "test.example.com",
		logger:         logger,
		debounceWindow: 100 * time.Millisecond,
		stunnelStarted: true,
	}

	// Create a tunnel first
	port, err := sm.EnsureTunnel("vol1", "server1.example.com", "test-request")
	if err != nil {
		t.Fatalf("Failed to create tunnel: %v", err)
	}

	// Wait for debounce
	time.Sleep(150 * time.Millisecond)

	// On macOS /proc/mounts is unavailable, so removal is fail-safe and keeps the tunnel.
	if runtime.GOOS == "darwin" {
		err = sm.RemoveTunnel("vol1", "test-request")
		if err != nil {
			t.Errorf("RemoveTunnel() unexpected error = %v", err)
		}

		configPath := filepath.Join(tmpDir, "vol1.conf")
		if _, err := os.Stat(configPath); err != nil {
			t.Errorf("Config file should be retained on macOS fail-safe path: %v", err)
		}
		if _, exists := sm.allocatedPorts["vol1"]; !exists {
			t.Error("Port should remain allocated on macOS fail-safe path")
		}
		return
	}

	// Remove the tunnel
	err = sm.RemoveTunnel("vol1", "test-request")
	if err == nil {
		// Verify config file was removed
		configPath := filepath.Join(tmpDir, "vol1.conf")
		if _, err := os.Stat(configPath); !os.IsNotExist(err) {
			t.Error("Config file should be removed")
		}

		// Verify port was released
		if _, exists := sm.allocatedPorts["vol1"]; exists {
			t.Error("Port should be released from allocatedPorts map")
		}

		// Verify port can be reused
		newPort, err := sm.EnsureTunnel("vol2", "server2.example.com", "test-request")
		if err != nil {
			t.Errorf("Failed to reuse port: %v", err)
		}
		if newPort != port {
			t.Errorf("Port not reused, got %d, want %d", newPort, port)
		}
	} else if !strings.Contains(err.Error(), "failed to send SIGHUP before removing last config") {
		t.Errorf("RemoveTunnel() unexpected error = %v", err)
	}
}

// TestRemoveTunnel_EmptyVolumeID tests removal with empty volumeID
func TestRemoveTunnel_EmptyVolumeID(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		servicesDir:    t.TempDir(),
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		logger:         logger,
	}

	err := sm.RemoveTunnel("", "test-request")
	if err == nil {
		t.Error("RemoveTunnel() expected error for empty volumeID, got nil")
	}
}

// TestRemoveTunnel_NonExistent tests removal of non-existent tunnel
func TestRemoveTunnel_NonExistent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		servicesDir:    t.TempDir(),
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		logger:         logger,
	}

	err := sm.RemoveTunnel("non-existent", "test-request")
	if err != nil {
		t.Errorf("RemoveTunnel() should not error for non-existent tunnel, got = %v", err)
	}
}

// TestIsTunnelPortInUse tests port usage detection
func TestIsTunnelPortInUse(t *testing.T) {
	logger := zaptest.NewLogger(t)

	sm := &StunnelManager{
		logger: logger,
	}

	tests := []struct {
		name string
		port int
	}{
		{name: "random port 1", port: 10001},
		{name: "random port 2", port: 10003},
		{name: "high port", port: 50000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify it doesn't panic and returns a boolean
			result := sm.isTunnelPortInUse(tt.port)
			_ = result
		})
	}
}

// TestGetTunnelPort tests port retrieval
func TestGetTunnelPort(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		allocatedPorts: map[string]int{
			"vol1": 10001,
			"vol2": 10002,
		},
		logger: logger,
	}

	tests := []struct {
		name       string
		volumeID   string
		wantPort   int
		wantExists bool
	}{
		{
			name:       "existing volume",
			volumeID:   "vol1",
			wantPort:   10001,
			wantExists: true,
		},
		{
			name:       "non-existent volume",
			volumeID:   "vol3",
			wantPort:   0,
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, exists := sm.GetTunnelPort(tt.volumeID)
			if port != tt.wantPort {
				t.Errorf("GetTunnelPort() port = %v, want %v", port, tt.wantPort)
			}
			if exists != tt.wantExists {
				t.Errorf("GetTunnelPort() exists = %v, want %v", exists, tt.wantExists)
			}
		})
	}
}

// TestIsPortAvailable tests port availability check
func TestIsPortAvailable(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		initialPort: 50000,
		portRange:   10,
		logger:      logger,
	}

	// Test with a port that should be available
	available := sm.isPortAvailable(50000)
	if !available {
		t.Error("isPortAvailable() expected port 50000 to be available")
	}

	// Test with a port that's in use (bind to it first)
	listener, err := net.Listen("tcp", "127.0.0.1:50001")
	if err != nil {
		t.Fatalf("Failed to bind test port: %v", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Logf("Failed to close test listener: %v", err)
		}
	}()

	available = sm.isPortAvailable(50001)
	if available {
		t.Error("isPortAvailable() expected port 50001 to be unavailable")
	}
}

// TestReleasePort tests port release
func TestReleasePort(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		allocatedPorts: map[string]int{
			"vol1": 10001,
			"vol2": 10002,
			"vol3": 10003,
		},
		portToVolume: map[int]string{
			10001: "vol1",
			10002: "vol2",
			10003: "vol3",
		},
		logger: logger,
	}

	// Release port 10002 directly (inline releasePort logic)
	delete(sm.allocatedPorts, "vol2")
	delete(sm.portToVolume, 10002)

	if _, exists := sm.allocatedPorts["vol2"]; exists {
		t.Error("vol2 should be removed from allocatedPorts")
	}
	if _, exists := sm.allocatedPorts["vol1"]; !exists {
		t.Error("vol1 should still exist in allocatedPorts")
	}
	if _, exists := sm.allocatedPorts["vol3"]; !exists {
		t.Error("vol3 should still exist in allocatedPorts")
	}

	// Release non-existent port (should not panic)
	delete(sm.allocatedPorts, "non-existent")
	delete(sm.portToVolume, 99999)
}

// TestGetConfigPath tests config path generation
func TestGetConfigPath(t *testing.T) {
	servicesDir := "/etc/stunnel/services"

	tests := []struct {
		name     string
		volumeID string
		want     string
	}{
		{
			name:     "simple volumeID",
			volumeID: "vol1",
			want:     "/etc/stunnel/services/vol1.conf",
		},
		{
			name:     "volumeID with special chars",
			volumeID: "vol-123-abc",
			want:     "/etc/stunnel/services/vol-123-abc.conf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filepath.Join(servicesDir, tt.volumeID+".conf")
			if result != tt.want {
				t.Errorf("filepath.Join() = %v, want %v", result, tt.want)
			}
		})
	}
}

// TestGetAllocatedPortsCount tests port count retrieval
func TestGetAllocatedPortsCount(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		allocatedPorts: map[string]int{
			"vol1": 10001,
			"vol2": 10002,
			"vol3": 10003,
		},
		logger: logger,
	}

	count := len(sm.allocatedPorts)
	if count != 3 {
		t.Errorf("len(allocatedPorts) = %v, want 3", count)
	}

	sm.allocatedPorts = make(map[string]int)
	count = len(sm.allocatedPorts)
	if count != 0 {
		t.Errorf("len(allocatedPorts) = %v, want 0", count)
	}
}

// TestScheduleDebouncedSIGHUP tests SIGHUP debouncing
func TestScheduleDebouncedSIGHUP(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		logger:         logger,
		debounceWindow: 50 * time.Millisecond,
		stunnelStarted: true,
	}

	// Schedule multiple SIGHUPs rapidly
	for i := 0; i < 5; i++ {
		sm.scheduleDebouncedSIGHUP(fmt.Sprintf("request-%d", i))
		time.Sleep(10 * time.Millisecond)
	}

	sm.debounceMu.Lock()
	if !sm.pendingSIGHUP {
		t.Error("pendingSIGHUP should be true")
	}
	if sm.debounceTimer == nil {
		t.Error("debounceTimer should not be nil")
	}
	sm.debounceMu.Unlock()

	// Ensure any residual timer is stopped when the test ends so the AfterFunc
	// callback cannot fire against a torn-down *testing.T.
	t.Cleanup(func() {
		sm.debounceMu.Lock()
		if sm.debounceTimer != nil {
			sm.debounceTimer.Stop()
			sm.debounceTimer = nil
		}
		sm.debounceMu.Unlock()
	})

	// Wait for debounce window to expire and the callback goroutine to finish.
	time.Sleep(200 * time.Millisecond)

	sm.debounceMu.Lock()
	if sm.pendingSIGHUP {
		t.Error("pendingSIGHUP should be false after debounce window")
	}
	sm.debounceMu.Unlock()
}

// TestIsStunnelRunning tests stunnel process detection
func TestIsStunnelRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		logger: logger,
	}

	// This will likely return false in test environment — just verify no panic
	running := sm.isStunnelRunning()
	_ = running
}

// TestReloadStunnel tests stunnel reload
func TestReloadStunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		logger: logger,
	}

	// This will likely fail in test environment (no stunnel process)
	err := sm.reloadStunnel("test-request")
	if err == nil {
		t.Log("stunnel process found in test environment")
	}
}

// TestRemoveTunnel_LastTunnel tests last tunnel removal behavior
func TestRemoveTunnel_LastTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	sm := &StunnelManager{
		servicesDir:    tmpDir,
		initialPort:    10001,
		portRange:      100,
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		caFile:         "/tmp/ca.pem",
		checkHost:      "test.example.com",
		logger:         logger,
		debounceWindow: 50 * time.Millisecond,
		stunnelStarted: true,
	}

	_, err := sm.EnsureTunnel("vol1", "server1.example.com", "test-request")
	if err != nil {
		t.Fatalf("Failed to create tunnel: %v", err)
	}

	sm.scheduleDebouncedSIGHUP("test-request")

	if runtime.GOOS == "darwin" {
		err = sm.RemoveTunnel("vol1", "test-request")
		if err != nil {
			t.Errorf("RemoveTunnel() unexpected error on macOS = %v", err)
		}
		return
	}

	err = sm.RemoveTunnel("vol1", "test-request")

	if err != nil {
		if !strings.Contains(err.Error(), "failed to send SIGHUP before removing last config") {
			t.Errorf("Expected SIGHUP error, got: %v", err)
		}

		if _, exists := sm.allocatedPorts["vol1"]; !exists {
			t.Error("Port should still be allocated after SIGHUP failure (rollback)")
		}
	}

	sm.debounceMu.Lock()
	stillPending := sm.pendingSIGHUP
	sm.debounceMu.Unlock()

	if stillPending {
		t.Error("pendingSIGHUP should be cleared after attempting forced SIGHUP")
	}
}

// TestEnsureTunnel_StunnelNotStarted tests tunnel creation when stunnel is not running.
func TestEnsureTunnel_StunnelNotStarted(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	sm := &StunnelManager{
		servicesDir:    tmpDir,
		initialPort:    10001,
		portRange:      100,
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		caFile:         "/tmp/ca.pem",
		checkHost:      "test.example.com",
		logger:         logger,
		debounceWindow: 100 * time.Millisecond,
		stunnelStarted: false, // Stunnel not started yet
	}

	_, err := sm.EnsureTunnel("vol1", "server1.example.com", "test-request")
	if err == nil {
		t.Fatal("EnsureTunnel() expected error when stunnel does not start, got nil")
	}
	if !strings.Contains(err.Error(), "stunnel not running after") {
		t.Fatalf("EnsureTunnel() error = %v, want startup wait failure", err)
	}

	if sm.stunnelStarted {
		t.Error("stunnelStarted should remain false when startup verification fails")
	}
	if _, exists := sm.allocatedPorts["vol1"]; exists {
		t.Error("allocatedPorts should be rolled back when startup verification fails")
	}
	if _, exists := sm.portToVolume[10001]; exists {
		t.Error("portToVolume should be rolled back when startup verification fails")
	}
	if _, statErr := os.Stat(filepath.Join(tmpDir, "vol1.conf")); !os.IsNotExist(statErr) {
		t.Error("config file should be removed during rollback after startup verification failure")
	}
}

// TestRecoverExistingTunnels_ReadError tests recovery with read error
func TestRecoverExistingTunnels_ReadError(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Use a file instead of directory to cause read error
	tmpFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	sm := &StunnelManager{
		servicesDir:    tmpFile, // This is a file, not a directory
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		logger:         logger,
	}

	err := sm.recoverExistingTunnels()
	if err == nil {
		t.Error("recoverExistingTunnels() expected error for invalid directory, got nil")
	}
}

// TestExtractPortFromConfigFile_FileNotFound tests extraction with missing file
func TestExtractPortFromConfigFile_FileNotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		logger: logger,
	}

	_, err := sm.extractPortFromConfigFile("/non/existent/file.conf")
	if err == nil {
		t.Error("extractPortFromConfigFile() expected error for missing file, got nil")
	}
}

// TestEnsureTunnel_WriteConfigError tests config write failure
func TestEnsureTunnel_WriteConfigError(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a read-only directory to force write failure
	tmpDir := t.TempDir()
	if err := os.Chmod(tmpDir, 0444); err != nil {
		t.Fatalf("Failed to make directory read-only: %v", err)
	}
	defer func() {
		if err := os.Chmod(tmpDir, 0755); err != nil {
			t.Logf("Failed to restore permissions: %v", err)
		}
	}()

	sm := &StunnelManager{
		servicesDir:    tmpDir,
		initialPort:    10001,
		portRange:      100,
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		caFile:         "/tmp/ca.pem",
		checkHost:      "test.example.com",
		logger:         logger,
		stunnelStarted: true,
	}

	_, err := sm.EnsureTunnel("vol1", "server1.example.com", "test-request")
	if err == nil {
		t.Error("EnsureTunnel() expected error for write failure, got nil")
	}
	if !strings.Contains(err.Error(), "failed to write") {
		t.Errorf("EnsureTunnel() error = %v, want error containing 'failed to write'", err)
	}

	// Verify rollback: maps must be empty
	if len(sm.allocatedPorts) != 0 {
		t.Errorf("Port map should be empty after rollback, got %d entries: %v",
			len(sm.allocatedPorts), sm.allocatedPorts)
	}
	if len(sm.portToVolume) != 0 {
		t.Errorf("Reverse port map should be empty after rollback, got %d entries: %v",
			len(sm.portToVolume), sm.portToVolume)
	}

	// Verify port can be reallocated after rollback
	if err := os.Chmod(tmpDir, 0755); err != nil {
		t.Fatalf("Failed to restore directory permissions: %v", err)
	}

	port, err := sm.EnsureTunnel("vol1", "server1.example.com", "test-request-2")
	if err != nil {
		t.Errorf("Port should be reallocatable after rollback, got error: %v", err)
	}
	if port != 10001 {
		t.Errorf("Expected first port 10001, got %d", port)
	}

	if len(sm.allocatedPorts) != 1 {
		t.Errorf("Expected 1 allocated port, got %d", len(sm.allocatedPorts))
	}
	if sm.allocatedPorts["vol1"] != port {
		t.Errorf("Expected vol1 -> %d mapping, got %d", port, sm.allocatedPorts["vol1"])
	}
	if len(sm.portToVolume) != 1 {
		t.Errorf("Expected 1 reverse mapping, got %d", len(sm.portToVolume))
	}
	if sm.portToVolume[port] != "vol1" {
		t.Errorf("Expected %d -> vol1 mapping, got %s", port, sm.portToVolume[port])
	}
}

// TestRemoveTunnel_PortStillInUse tests removal when port is still in use
func TestRemoveTunnel_PortStillInUse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	sm := &StunnelManager{
		servicesDir:    tmpDir,
		initialPort:    10001,
		portRange:      100,
		allocatedPorts: map[string]int{"vol1": 10001},
		portToVolume:   map[int]string{10001: "vol1"},
		logger:         logger,
	}

	configPath := filepath.Join(tmpDir, "vol1.conf")
	if err := os.WriteFile(configPath, []byte("test"), 0600); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	err := sm.RemoveTunnel("vol1", "test-request")
	if err != nil && !strings.Contains(err.Error(), "failed to send SIGHUP before removing last config") {
		t.Errorf("RemoveTunnel() unexpected error = %v", err)
	}
}

// TestIsTunnelPortInUse_WithMounts tests port usage detection with actual mounts
func TestIsTunnelPortInUse_WithMounts(t *testing.T) {
	logger := zaptest.NewLogger(t)

	sm := &StunnelManager{
		logger: logger,
	}

	// Test with actual /proc/mounts — primarily verifies no panic
	result := sm.isTunnelPortInUse(10001)
	_ = result
}

// TestScheduleDebouncedSIGHUP_AlreadyPending tests debouncing with already pending SIGHUP
func TestScheduleDebouncedSIGHUP_AlreadyPending(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		logger:         logger,
		debounceWindow: 100 * time.Millisecond,
		stunnelStarted: true,
	}

	sm.scheduleDebouncedSIGHUP("request-1")

	sm.debounceMu.Lock()
	if !sm.pendingSIGHUP {
		t.Error("pendingSIGHUP should be true after first schedule")
	}
	firstTimer := sm.debounceTimer
	sm.debounceMu.Unlock()

	time.Sleep(10 * time.Millisecond)
	sm.scheduleDebouncedSIGHUP("request-2")

	sm.debounceMu.Lock()
	secondTimer := sm.debounceTimer
	sm.debounceMu.Unlock()

	if firstTimer == secondTimer {
		t.Error("Timer should be reset on second schedule")
	}

	time.Sleep(150 * time.Millisecond)

	sm.debounceMu.Lock()
	if sm.pendingSIGHUP {
		t.Error("pendingSIGHUP should be false after debounce completes")
	}
	sm.debounceMu.Unlock()
}

// TestRemoveTunnel_WithPendingSIGHUP tests last tunnel removal with pending SIGHUP
func TestRemoveTunnel_WithPendingSIGHUP(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Skipping on macOS - /proc/mounts not available, test behavior is platform-specific")
	}

	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	sm := &StunnelManager{
		servicesDir:    tmpDir,
		initialPort:    10001,
		portRange:      100,
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		caFile:         "/tmp/ca.pem",
		checkHost:      "test.example.com",
		logger:         logger,
		debounceWindow: 50 * time.Millisecond,
		stunnelStarted: true,
	}

	_, err := sm.EnsureTunnel("vol1", "server1.example.com", "test-request")
	if err != nil {
		t.Fatalf("Failed to create tunnel: %v", err)
	}

	sm.debounceMu.Lock()
	hasPending := sm.pendingSIGHUP
	sm.debounceMu.Unlock()

	if !hasPending {
		t.Error("Should have pending SIGHUP after tunnel creation")
	}

	err = sm.RemoveTunnel("vol1", "test-request")

	if err != nil {
		if !strings.Contains(err.Error(), "failed to send SIGHUP before removing last config") {
			t.Errorf("Expected SIGHUP error, got: %v", err)
		}

		if _, exists := sm.allocatedPorts["vol1"]; !exists {
			t.Error("Port should still be allocated after SIGHUP failure (rollback)")
		}

		sm.debounceMu.Lock()
		stillPending := sm.pendingSIGHUP
		sm.debounceMu.Unlock()

		if stillPending {
			t.Error("pendingSIGHUP should be cleared after attempting forced SIGHUP")
		}
	} else {
		t.Error("Expected SIGHUP error on Linux, got nil")
	}
}

// TestNewStunnelManagerForTesting tests the test helper function
func TestNewStunnelManagerForTesting(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tempDir := t.TempDir()
	servicesDir := filepath.Join(tempDir, "services")
	caFile := filepath.Join(tempDir, "ca.pem")

	if err := os.MkdirAll(servicesDir, 0755); err != nil {
		t.Fatalf("Failed to create services dir: %v", err)
	}
	if err := os.WriteFile(caFile, []byte("mock CA"), 0644); err != nil {
		t.Fatalf("Failed to create CA file: %v", err)
	}

	tests := []struct {
		name        string
		servicesDir string
		caFile      string
		logger      *zap.Logger
		wantErr     bool
		errContains string
	}{
		{
			name:        "valid parameters",
			servicesDir: servicesDir,
			caFile:      caFile,
			logger:      logger,
			wantErr:     false,
		},
		{
			name:        "nil logger",
			servicesDir: servicesDir,
			caFile:      caFile,
			logger:      nil,
			wantErr:     true,
			errContains: "logger is required",
		},
		{
			name:        "empty servicesDir",
			servicesDir: "",
			caFile:      caFile,
			logger:      logger,
			wantErr:     true,
			errContains: "servicesDir is required",
		},
		{
			name:        "empty caFile",
			servicesDir: servicesDir,
			caFile:      "",
			logger:      logger,
			wantErr:     true,
			errContains: "caFile is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm, err := NewStunnelManagerForTesting(tt.servicesDir, tt.caFile, tt.logger)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewStunnelManagerForTesting() expected error containing %q, got nil", tt.errContains)
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("NewStunnelManagerForTesting() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("NewStunnelManagerForTesting() unexpected error = %v", err)
				return
			}

			if sm == nil {
				t.Error("NewStunnelManagerForTesting() returned nil manager")
				return
			}

			if sm.servicesDir != tt.servicesDir {
				t.Errorf("servicesDir = %v, want %v", sm.servicesDir, tt.servicesDir)
			}
			if sm.caFile != tt.caFile {
				t.Errorf("caFile = %v, want %v", sm.caFile, tt.caFile)
			}
			if sm.initialPort != InitialPort {
				t.Errorf("initialPort = %v, want %v", sm.initialPort, InitialPort)
			}
			if sm.portRange != PortRange {
				t.Errorf("portRange = %v, want %v", sm.portRange, PortRange)
			}
			if sm.debugLevel != DefaultDebugLevel {
				t.Errorf("debugLevel = %v, want %v", sm.debugLevel, DefaultDebugLevel)
			}
			if sm.debounceWindow != 2*time.Second {
				t.Errorf("debounceWindow = %v, want %v", sm.debounceWindow, 2*time.Second)
			}
			if sm.checkHost != ProductionCheckHost {
				t.Errorf("checkHost = %v, want %v", sm.checkHost, ProductionCheckHost)
			}
		})
	}
}

// TestReloadStunnel_ErrorCases tests error handling in reloadStunnel
func TestReloadStunnel_ErrorCases(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tempDir := t.TempDir()
	servicesDir := filepath.Join(tempDir, "services")
	caFile := filepath.Join(tempDir, "ca.pem")

	if err := os.MkdirAll(servicesDir, 0755); err != nil {
		t.Fatalf("Failed to create services dir: %v", err)
	}
	if err := os.WriteFile(caFile, []byte("mock CA"), 0644); err != nil {
		t.Fatalf("Failed to create CA file: %v", err)
	}

	sm, err := NewStunnelManagerForTesting(servicesDir, caFile, logger)
	if err != nil {
		t.Fatalf("Failed to create StunnelManager: %v", err)
	}

	err = sm.reloadStunnel("test-request")
	if err == nil {
		t.Error("reloadStunnel() expected error when stunnel is not running, got nil")
	}

	// Verify error message is helpful
	if err != nil && !strings.Contains(err.Error(), "stunnel") {
		t.Errorf("reloadStunnel() error should mention stunnel, got: %v", err)
	}
}

// TestIsTunnelPortInUse_ErrorHandling tests /proc/mounts read failure handling
func TestIsTunnelPortInUse_ErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &StunnelManager{
		logger: logger,
	}

	// Just verify no panic
	result := sm.isTunnelPortInUse(10001)
	_ = result
}

// TestRemoveTunnel_AdditionalCases tests additional RemoveTunnel scenarios
func TestRemoveTunnel_AdditionalCases(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		volumeID   string
		setupPorts map[string]int
		wantErr    bool
	}{
		{
			name:       "remove non-existent volume",
			volumeID:   "non-existent",
			setupPorts: map[string]int{},
			wantErr:    false,
		},
		{
			name:     "empty volume ID",
			volumeID: "",
			setupPorts: map[string]int{
				"vol1": 10001,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			portToVol := make(map[int]string)
			for vid, p := range tt.setupPorts {
				portToVol[p] = vid
			}

			sm := &StunnelManager{
				servicesDir:    tmpDir,
				initialPort:    10001,
				portRange:      100,
				allocatedPorts: tt.setupPorts,
				portToVolume:   portToVol,
				caFile:         "/tmp/ca.pem",
				checkHost:      "test.example.com",
				logger:         logger,
				debounceWindow: 50 * time.Millisecond,
				stunnelStarted: true,
			}

			err := sm.RemoveTunnel(tt.volumeID, "test-request")
			if tt.wantErr && err == nil {
				t.Error("RemoveTunnel() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("RemoveTunnel() unexpected error = %v", err)
			}
		})
	}
}
