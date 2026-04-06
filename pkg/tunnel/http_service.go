package tunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	healthEndpoint = "/healthz"
	tunnelBasePath = "/v1/tunnels"
)

type ensureTunnelRequest struct {
	VolumeID  string `json:"volumeID"`
	NFSServer string `json:"nfsServer"`
}

type removeTunnelRequest struct {
	VolumeID string `json:"volumeID"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// HTTPClient implements Service over HTTP on a Unix domain socket.
type HTTPClient struct {
	socketPath string
	baseURL    string
	client     *http.Client
	logger     *zap.Logger
}

// NewHTTPClient creates a Unix-socket based tunnel service client.
func NewHTTPClient(socketPath string, logger *zap.Logger) *HTTPClient {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}

	return &HTTPClient{
		socketPath: socketPath,
		baseURL:    "http://unix",
		client: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		},
		logger: logger,
	}
}

// EnsureTunnel ensures a tunnel exists for the given volume.
func (c *HTTPClient) EnsureTunnel(ctx context.Context, volumeID, nfsServer string) (*TunnelInfo, error) {
	reqBody := ensureTunnelRequest{
		VolumeID:  volumeID,
		NFSServer: nfsServer,
	}

	var resp TunnelInfo
	if err := c.doJSON(ctx, http.MethodPost, tunnelBasePath+"/ensure", reqBody, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RemoveTunnel decrements tunnel refcount or removes it entirely.
func (c *HTTPClient) RemoveTunnel(ctx context.Context, volumeID string) error {
	reqBody := removeTunnelRequest{
		VolumeID: volumeID,
	}
	return c.doJSON(ctx, http.MethodPost, tunnelBasePath+"/remove", reqBody, nil)
}

// GetTunnel fetches a tunnel by volume ID.
func (c *HTTPClient) GetTunnel(ctx context.Context, volumeID string) (*TunnelInfo, bool, error) {
	var resp TunnelInfo
	err := c.doJSON(ctx, http.MethodGet, path.Join(tunnelBasePath, volumeID), nil, &resp)
	if err != nil {
		if strings.Contains(err.Error(), "status 404") {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &resp, true, nil
}

// Health checks tunnel-manager server liveness.
func (c *HTTPClient) Health(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodGet, healthEndpoint, nil, nil)
}

func (c *HTTPClient) doJSON(ctx context.Context, method, endpoint string, reqBody interface{}, respBody interface{}) error {
	var bodyReader *bytes.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("tunnel-manager request failed on socket %s: %w", c.socketPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		var errResp errorResponse
		if decodeErr := json.NewDecoder(resp.Body).Decode(&errResp); decodeErr == nil && errResp.Error != "" {
			return fmt.Errorf("tunnel-manager returned status %d: %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("tunnel-manager returned status %d", resp.StatusCode)
	}

	if respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
			return fmt.Errorf("failed to decode response body: %w", err)
		}
	}

	return nil
}

// HTTPServer exposes Manager operations over HTTP on a Unix domain socket.
type HTTPServer struct {
	manager    *Manager
	socketPath string
	logger     *zap.Logger
	server     *http.Server
	listener   net.Listener
}

// NewHTTPServer creates a new Unix-socket HTTP server around a tunnel manager.
func NewHTTPServer(manager *Manager, socketPath string, logger *zap.Logger) *HTTPServer {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	s := &HTTPServer{
		manager:    manager,
		socketPath: socketPath,
		logger:     logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc(healthEndpoint, s.handleHealth)
	mux.HandleFunc(tunnelBasePath+"/ensure", s.handleEnsureTunnel)
	mux.HandleFunc(tunnelBasePath+"/remove", s.handleRemoveTunnel)
	mux.HandleFunc(tunnelBasePath+"/", s.handleGetTunnel)

	s.server = &http.Server{
		Handler: mux,
	}

	return s
}

// Start starts the Unix-socket HTTP server.
func (s *HTTPServer) Start() error {
	if s.manager == nil {
		return errors.New("tunnel manager is nil")
	}

	if err := os.MkdirAll(path.Dir(s.socketPath), 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove stale socket: %w", err)
	}

	l, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on unix socket: %w", err)
	}
	s.listener = l

	if err := os.Chmod(s.socketPath, 0660); err != nil {
		s.listener.Close()
		return fmt.Errorf("failed to chmod socket: %w", err)
	}

	s.logger.Info("Starting tunnel-manager HTTP server", zap.String("socketPath", s.socketPath))

	go func() {
		if err := s.server.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("Tunnel-manager HTTP server stopped unexpectedly", zap.Error(err))
		}
	}()

	return nil
}

// Stop gracefully stops the Unix-socket HTTP server.
func (s *HTTPServer) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	err := s.server.Shutdown(ctx)
	if s.listener != nil {
		_ = s.listener.Close()
	}
	if removeErr := os.Remove(s.socketPath); removeErr != nil && !os.IsNotExist(removeErr) {
		s.logger.Warn("Failed to remove tunnel-manager socket", zap.Error(removeErr))
	}
	return err
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *HTTPServer) handleEnsureTunnel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ensureTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to decode request: %v", err))
		return
	}
	if req.VolumeID == "" || req.NFSServer == "" {
		writeError(w, http.StatusBadRequest, "volumeID and nfsServer are required")
		return
	}

	tun, err := s.manager.EnsureTunnel(req.VolumeID, req.NFSServer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, ToTunnelInfo(tun))
}

func (s *HTTPServer) handleRemoveTunnel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req removeTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to decode request: %v", err))
		return
	}
	if req.VolumeID == "" {
		writeError(w, http.StatusBadRequest, "volumeID is required")
		return
	}

	if err := s.manager.RemoveTunnel(req.VolumeID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *HTTPServer) handleGetTunnel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	volumeID := strings.TrimPrefix(r.URL.Path, tunnelBasePath+"/")
	if volumeID == "" {
		writeError(w, http.StatusBadRequest, "volumeID is required")
		return
	}

	tun, ok := s.manager.GetTunnel(volumeID)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("tunnel not found for volume %s", volumeID))
		return
	}

	writeJSON(w, http.StatusOK, ToTunnelInfo(tun))
}

func writeJSON(w http.ResponseWriter, statusCode int, obj interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(obj)
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, errorResponse{Error: message})
}

// Made with Bob
