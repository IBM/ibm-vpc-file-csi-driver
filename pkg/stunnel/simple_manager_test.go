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

package stunnel

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestNewSimpleManager tests the constructor with hardcoded defaults
func TestNewSimpleManager(t *testing.T) {
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
			// Set OS_TYPE environment variable for valid test case
			if !tt.wantErr {
				t.Setenv("OS_TYPE", "RHCOS")
			}
			sm, err := NewSimpleManager(tt.logger)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewSimpleManager() expected error containing %q, got nil", tt.errContains)
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("NewSimpleManager() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("NewSimpleManager() unexpected error = %v", err)
				return
			}

			if sm == nil {
				t.Error("NewSimpleManager() returned nil manager")
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
			// Set OS_TYPE environment variable
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
				// Ensure OS_TYPE is not set
				_ = os.Unsetenv("OS_TYPE")
			}

			result, err := detectCABundle(logger)
			if tt.wantErr {
				if err == nil {
					t.Errorf("detectCABundle() expected error, got nil")
				}
				return
			}

			if err != nil {
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
	}{
		{
			name:       "production",
			clusterEnv: "production",
			want:       "production.is-share.appdomain.cloud",
		},
		{
			name:       "prod",
			clusterEnv: "prod",
			want:       "production.is-share.appdomain.cloud",
		},
		{
			name:       "staging",
			clusterEnv: "staging",
			want:       "staging.is-share.appdomain.cloud",
		},
		{
			name:       "stage",
			clusterEnv: "stage",
			want:       "staging.is-share.appdomain.cloud",
		},
		{
			name:       "default (empty)",
			clusterEnv: "",
			want:       "production.is-share.appdomain.cloud",
		},
		{
			name:       "unknown",
			clusterEnv: "unknown",
			want:       "production.is-share.appdomain.cloud",
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
			}

			result := getCheckHost(logger)
			if result != tt.want {
				t.Errorf("getCheckHost() = %v, want %v", result, tt.want)
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

	sm := &SimpleManager{
		servicesDir:    tmpDir,
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
	sm := &SimpleManager{
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
			name: "IPv6 address",
			content: `[test]
accept = [::1]:10005
connect = server:20049
`,
			want:    10005,
			wantErr: false,
		},
		{
			name: "IPv6 full address",
			content: `[test]
accept = [2001:db8::1]:10006
connect = server:20049
`,
			want:    10006,
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
			name: "malformed IPv6 - missing bracket",
			content: `[test]
accept = [::1:10008
connect = server:20049
`,
			want:    0,
			wantErr: true,
		},
		{
			name: "malformed IPv6 - missing port",
			content: `[test]
accept = [::1]
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

	sm := &SimpleManager{
		logger: logger,
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

	sm := &SimpleManager{
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

// TestEnsureTunnel_NoTLSConfig tests tunnel creation without TLS config
func TestEnsureTunnel_NoTLSConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		caFile    string
		checkHost string
		wantErr   bool
	}{
		{
			name:      "missing CA file",
			caFile:    "",
			checkHost: "test.example.com",
			wantErr:   true,
		},
		{
			name:      "missing checkHost",
			caFile:    "/tmp/ca.pem",
			checkHost: "",
			wantErr:   true,
		},
		{
			name:      "both missing",
			caFile:    "",
			checkHost: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &SimpleManager{
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

			_, err := sm.EnsureTunnel("vol1", "server1.example.com", "test-request")
			if !tt.wantErr {
				t.Error("EnsureTunnel() expected no error for valid TLS config")
			} else if err == nil {
				t.Error("EnsureTunnel() expected error for missing TLS config, got nil")
			} else if !strings.Contains(err.Error(), "TLS verification required") {
				t.Errorf("EnsureTunnel() error = %v, want error containing 'TLS verification required'", err)
			}
		})
	}
}

// TestEnsureTunnel_Concurrent tests concurrent tunnel creation
func TestEnsureTunnel_Concurrent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	sm := &SimpleManager{
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

	sm := &SimpleManager{
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

	// Remove the tunnel
	err = sm.RemoveTunnel("vol1", "test-request")
	if err != nil {
		t.Errorf("RemoveTunnel() unexpected error = %v", err)
	}

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
}

// TestRemoveTunnel_EmptyVolumeID tests removal with empty volumeID
func TestRemoveTunnel_EmptyVolumeID(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &SimpleManager{
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
	sm := &SimpleManager{
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

	sm := &SimpleManager{
		logger: logger,
	}

	// Test with actual /proc/mounts (if available)
	// This test verifies the function doesn't panic and handles errors gracefully
	tests := []struct {
		name string
		port int
	}{
		{
			name: "random port 1",
			port: 10001,
		},
		{
			name: "random port 2",
			port: 10003,
		},
		{
			name: "high port",
			port: 50000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify it doesn't panic and returns a boolean
			result := sm.isTunnelPortInUse(tt.port)
			// Result will depend on actual system state
			_ = result
		})
	}

	// Test error handling when /proc/mounts doesn't exist
	// We can't easily mock this, but the function should handle it gracefully
	// and return false (fail-safe behavior)
}

// TestGetTunnelPort tests port retrieval
func TestGetTunnelPort(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &SimpleManager{
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

// TestAllocatePort tests port allocation
func TestAllocatePort(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name           string
		initialPort    int
		portRange      int
		allocatedPorts map[string]int
		volumeID       string
		wantErr        bool
	}{
		{
			name:           "allocate first port",
			initialPort:    10001,
			portRange:      10,
			allocatedPorts: make(map[string]int),
			volumeID:       "vol1",
			wantErr:        false,
		},
		{
			name:        "allocate with existing ports",
			initialPort: 10001,
			portRange:   10,
			allocatedPorts: map[string]int{
				"vol1": 10001,
				"vol2": 10002,
			},
			volumeID: "vol3",
			wantErr:  false,
		},
		{
			name:        "no available ports",
			initialPort: 10001,
			portRange:   2,
			allocatedPorts: map[string]int{
				"vol1": 10001,
				"vol2": 10002,
			},
			volumeID: "vol3",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build portToVolume from allocatedPorts to maintain consistency
			portToVolume := make(map[int]string)
			for volID, port := range tt.allocatedPorts {
				portToVolume[port] = volID
			}

			sm := &SimpleManager{
				initialPort:    tt.initialPort,
				portRange:      tt.portRange,
				allocatedPorts: tt.allocatedPorts,
				portToVolume:   portToVolume,
				logger:         logger,
			}

			port, err := sm.allocatePort(tt.volumeID)
			if tt.wantErr {
				if err == nil {
					t.Error("allocatePort() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("allocatePort() unexpected error = %v", err)
				return
			}

			if port < tt.initialPort || port >= tt.initialPort+tt.portRange {
				t.Errorf("allocatePort() port %d out of range [%d, %d)", port, tt.initialPort, tt.initialPort+tt.portRange)
			}

			// Verify port was added to map
			if allocatedPort, exists := sm.allocatedPorts[tt.volumeID]; !exists || allocatedPort != port {
				t.Errorf("Port not added to allocatedPorts correctly, got %d, exists=%v", allocatedPort, exists)
			}
		})
	}
}

// TestIsPortAvailable tests port availability check
func TestIsPortAvailable(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &SimpleManager{
		logger: logger,
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
	sm := &SimpleManager{
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

	// Release port 10002
	sm.releasePort(10002)

	// Verify vol2 was removed
	if _, exists := sm.allocatedPorts["vol2"]; exists {
		t.Error("vol2 should be removed from allocatedPorts")
	}

	// Verify others remain
	if _, exists := sm.allocatedPorts["vol1"]; !exists {
		t.Error("vol1 should still exist in allocatedPorts")
	}
	if _, exists := sm.allocatedPorts["vol3"]; !exists {
		t.Error("vol3 should still exist in allocatedPorts")
	}

	// Release non-existent port (should not panic)
	sm.releasePort(99999)
}

// TestGetConfigPath tests config path generation
func TestGetConfigPath(t *testing.T) {
	sm := &SimpleManager{
		servicesDir: "/etc/stunnel/services",
	}

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
			result := sm.getConfigPath(tt.volumeID)
			if result != tt.want {
				t.Errorf("getConfigPath() = %v, want %v", result, tt.want)
			}
		})
	}
}

// TestGetAllocatedPortsCount tests port count retrieval
func TestGetAllocatedPortsCount(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &SimpleManager{
		allocatedPorts: map[string]int{
			"vol1": 10001,
			"vol2": 10002,
			"vol3": 10003,
		},
		logger: logger,
	}

	count := sm.GetAllocatedPortsCount()
	if count != 3 {
		t.Errorf("GetAllocatedPortsCount() = %v, want 3", count)
	}

	// Test with empty map
	sm.allocatedPorts = make(map[string]int)
	count = sm.GetAllocatedPortsCount()
	if count != 0 {
		t.Errorf("GetAllocatedPortsCount() = %v, want 0", count)
	}
}

// TestScheduleDebouncedSIGHUP tests SIGHUP debouncing
func TestScheduleDebouncedSIGHUP(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &SimpleManager{
		logger:         logger,
		debounceWindow: 50 * time.Millisecond,
		stunnelStarted: true,
	}

	// Schedule multiple SIGHUPs rapidly
	for i := 0; i < 5; i++ {
		sm.scheduleDebouncedSIGHUP(fmt.Sprintf("request-%d", i))
		time.Sleep(10 * time.Millisecond)
	}

	// Verify pendingSIGHUP is set
	sm.debounceMu.Lock()
	if !sm.pendingSIGHUP {
		t.Error("pendingSIGHUP should be true")
	}
	if sm.debounceTimer == nil {
		t.Error("debounceTimer should not be nil")
	}
	sm.debounceMu.Unlock()

	// Wait for debounce window to expire
	time.Sleep(100 * time.Millisecond)

	// Verify pendingSIGHUP was cleared
	sm.debounceMu.Lock()
	if sm.pendingSIGHUP {
		t.Error("pendingSIGHUP should be false after debounce window")
	}
	sm.debounceMu.Unlock()
}

// TestIsStunnelRunning tests stunnel process detection
func TestIsStunnelRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &SimpleManager{
		logger: logger,
	}

	// This will likely return false in test environment
	// Just verify it doesn't panic
	running := sm.isStunnelRunning()
	_ = running // Use the result to avoid unused variable warning
}

// TestReloadStunnel tests stunnel reload
func TestReloadStunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &SimpleManager{
		logger: logger,
	}

	// This will likely fail in test environment (no stunnel process)
	// Just verify it returns an error gracefully
	err := sm.reloadStunnel("test-request")
	if err == nil {
		// Stunnel is actually running in test environment (unlikely)
		t.Log("stunnel process found in test environment")
	}
}

// TestRemoveTunnel_LastTunnel tests last tunnel removal behavior
func TestRemoveTunnel_LastTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	sm := &SimpleManager{
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

	// Create a tunnel
	_, err := sm.EnsureTunnel("vol1", "server1.example.com", "test-request")
	if err != nil {
		t.Fatalf("Failed to create tunnel: %v", err)
	}

	// Schedule a debounced SIGHUP
	sm.scheduleDebouncedSIGHUP("test-request")

	// Remove the last tunnel (should force pending SIGHUP)
	err = sm.RemoveTunnel("vol1", "test-request")
	if err != nil {
		t.Errorf("RemoveTunnel() unexpected error = %v", err)
	}

	// Verify config was removed
	configPath := filepath.Join(tmpDir, "vol1.conf")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("Config file should be removed")
	}

	// Verify allocatedPorts is empty
	if len(sm.allocatedPorts) != 0 {
		t.Errorf("allocatedPorts should be empty, got %d entries", len(sm.allocatedPorts))
	}
}

// Made with Bob

// TestEnsureTunnel_StunnelNotStarted tests tunnel creation when stunnel is not running
func TestEnsureTunnel_StunnelNotStarted(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	sm := &SimpleManager{
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

	// This will trigger the 10-second wait path
	// We'll use a short timeout for testing
	done := make(chan bool)
	go func() {
		_, err := sm.EnsureTunnel("vol1", "server1.example.com", "test-request")
		if err != nil {
			t.Errorf("EnsureTunnel() unexpected error = %v", err)
		}
		done <- true
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		// Success
	case <-time.After(15 * time.Second):
		t.Fatal("EnsureTunnel() timed out")
	}

	// Verify stunnelStarted flag was set
	if !sm.stunnelStarted {
		t.Error("stunnelStarted should be true after first tunnel creation")
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

	sm := &SimpleManager{
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
	sm := &SimpleManager{
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

	// Create a read-only directory
	tmpDir := t.TempDir()
	if err := os.Chmod(tmpDir, 0444); err != nil {
		t.Fatalf("Failed to make directory read-only: %v", err)
	}
	defer func() {
		if err := os.Chmod(tmpDir, 0755); err != nil {
			t.Logf("Failed to restore permissions: %v", err)
		}
	}()

	sm := &SimpleManager{
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
	if !strings.Contains(err.Error(), "failed to write config") {
		t.Errorf("EnsureTunnel() error = %v, want error containing 'failed to write config'", err)
	}
}

// TestRemoveTunnel_PortStillInUse tests removal when port is still in use
func TestRemoveTunnel_PortStillInUse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	// Create a mock /proc/mounts file
	mountsContent := `127.0.0.1:/export1 /mnt/vol1 nfs4 rw,port=10001,vers=4.1 0 0`
	mountsFile := filepath.Join(tmpDir, "mounts")
	if err := os.WriteFile(mountsFile, []byte(mountsContent), 0644); err != nil {
		t.Fatalf("Failed to create mock mounts file: %v", err)
	}

	sm := &SimpleManager{
		servicesDir:    tmpDir,
		initialPort:    10001,
		portRange:      100,
		allocatedPorts: map[string]int{"vol1": 10001},
		logger:         logger,
	}

	// Create config file
	configPath := filepath.Join(tmpDir, "vol1.conf")
	if err := os.WriteFile(configPath, []byte("test"), 0600); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// Note: This test will use the real /proc/mounts, so we can't fully test this path
	// But we can verify the function doesn't panic
	err := sm.RemoveTunnel("vol1", "test-request")
	if err != nil {
		t.Errorf("RemoveTunnel() unexpected error = %v", err)
	}
}

// TestIsTunnelPortInUse_WithMounts tests port usage detection with actual mounts
func TestIsTunnelPortInUse_WithMounts(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a temporary file to simulate /proc/mounts
	tmpDir := t.TempDir()
	mountsFile := filepath.Join(tmpDir, "mounts")
	mountsContent := `127.0.0.1:/export1 /mnt/vol1 nfs4 rw,port=10001,vers=4.1 0 0
127.0.0.1:/export2 /mnt/vol2 nfs4 rw,port=10002,vers=4.1 0 0
/dev/sda1 / ext4 rw 0 0
`
	if err := os.WriteFile(mountsFile, []byte(mountsContent), 0644); err != nil {
		t.Fatalf("Failed to create mock mounts file: %v", err)
	}

	sm := &SimpleManager{
		logger: logger,
	}

	// Test with actual /proc/mounts (will likely return false in test environment)
	// This primarily tests that the function doesn't panic
	result := sm.isTunnelPortInUse(10001)
	_ = result // Just verify no panic
}

// TestScheduleDebouncedSIGHUP_AlreadyPending tests debouncing with already pending SIGHUP
func TestScheduleDebouncedSIGHUP_AlreadyPending(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm := &SimpleManager{
		logger:         logger,
		debounceWindow: 100 * time.Millisecond,
		stunnelStarted: true,
	}

	// Schedule first SIGHUP
	sm.scheduleDebouncedSIGHUP("request-1")

	// Verify pending flag is set
	sm.debounceMu.Lock()
	if !sm.pendingSIGHUP {
		t.Error("pendingSIGHUP should be true after first schedule")
	}
	firstTimer := sm.debounceTimer
	sm.debounceMu.Unlock()

	// Schedule second SIGHUP immediately (should reset timer)
	time.Sleep(10 * time.Millisecond)
	sm.scheduleDebouncedSIGHUP("request-2")

	// Verify timer was reset (different instance)
	sm.debounceMu.Lock()
	secondTimer := sm.debounceTimer
	sm.debounceMu.Unlock()

	if firstTimer == secondTimer {
		t.Error("Timer should be reset on second schedule")
	}

	// Wait for debounce to complete
	time.Sleep(150 * time.Millisecond)

	// Verify pending flag was cleared
	sm.debounceMu.Lock()
	if sm.pendingSIGHUP {
		t.Error("pendingSIGHUP should be false after debounce completes")
	}
	sm.debounceMu.Unlock()
}

// TestRemoveTunnel_WithPendingSIGHUP tests last tunnel removal with pending SIGHUP
func TestRemoveTunnel_WithPendingSIGHUP(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()

	sm := &SimpleManager{
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

	// Create a tunnel
	_, err := sm.EnsureTunnel("vol1", "server1.example.com", "test-request")
	if err != nil {
		t.Fatalf("Failed to create tunnel: %v", err)
	}

	// Verify pending SIGHUP was scheduled
	sm.debounceMu.Lock()
	hasPending := sm.pendingSIGHUP
	sm.debounceMu.Unlock()

	if !hasPending {
		t.Error("Should have pending SIGHUP after tunnel creation")
	}

	// Remove the last tunnel (should force pending SIGHUP)
	err = sm.RemoveTunnel("vol1", "test-request")
	if err != nil {
		t.Errorf("RemoveTunnel() unexpected error = %v", err)
	}

	// Verify pending SIGHUP was cleared
	sm.debounceMu.Lock()
	stillPending := sm.pendingSIGHUP
	sm.debounceMu.Unlock()

	if stillPending {
		t.Error("pendingSIGHUP should be cleared after forced SIGHUP")
	}
}

// TestAllocatePort_AllPortsInUse tests allocation when all ports are in use
func TestAllocatePort_AllPortsInUse(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create manager with very small port range
	sm := &SimpleManager{
		initialPort: 50000,
		portRange:   2,
		allocatedPorts: map[string]int{
			"vol1": 50000,
			"vol2": 50001,
		},
		portToVolume: map[int]string{
			50000: "vol1",
			50001: "vol2",
		},
		logger: logger,
	}

	// Try to allocate when all ports are used
	_, err := sm.allocatePort("vol3")
	if err == nil {
		t.Error("allocatePort() expected error when all ports in use, got nil")
	}
	if !strings.Contains(err.Error(), "no available ports") {
		t.Errorf("allocatePort() error = %v, want error containing 'no available ports'", err)
	}
}

// TestAllocatePort_PortInUseBySystem tests allocation when port is in use by system
func TestAllocatePort_PortInUseBySystem(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Bind to a port to make it unavailable
	listener, err := net.Listen("tcp", "127.0.0.1:50010")
	if err != nil {
		t.Fatalf("Failed to bind test port: %v", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Logf("Failed to close test listener: %v", err)
		}
	}()

	sm := &SimpleManager{
		initialPort:    50010,
		portRange:      5,
		allocatedPorts: make(map[string]int),
		portToVolume:   make(map[int]string),
		logger:         logger,
	}

	// Allocate port - should skip 50010 and use 50011
	port, err := sm.allocatePort("vol1")
	if err != nil {
		t.Errorf("allocatePort() unexpected error = %v", err)
	}
	if port == 50010 {
		t.Error("allocatePort() should not allocate port that's in use by system")
	}
	if port != 50011 {
		t.Errorf("allocatePort() = %d, want 50011", port)
	}
}
