package tunnel

import (
	"context"
	"fmt"
	"net"
	"os"

	pb "github.com/IBM/ibm-vpc-file-csi-driver/pkg/tunnel/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCServer implements the TunnelManager gRPC service
type GRPCServer struct {
	pb.UnimplementedTunnelManagerServer
	manager    *Manager
	socketPath string
	server     *grpc.Server
	listener   net.Listener
	logger     *zap.Logger
}

// NewGRPCServer creates a new gRPC server for tunnel management
func NewGRPCServer(manager *Manager, socketPath string, logger *zap.Logger) *GRPCServer {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	return &GRPCServer{
		manager:    manager,
		socketPath: socketPath,
		logger:     logger,
	}
}

// Start starts the gRPC server on the Unix socket
func (s *GRPCServer) Start() error {
	// Remove existing socket file if it exists
	if err := os.RemoveAll(s.socketPath); err != nil {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	// Create Unix socket listener
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s: %w", s.socketPath, err)
	}
	s.listener = listener

	// Set socket permissions
	if err := os.Chmod(s.socketPath, 0660); err != nil {
		s.listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	// Create gRPC server
	s.server = grpc.NewServer(
		grpc.MaxRecvMsgSize(1024*1024), // 1MB
		grpc.MaxSendMsgSize(1024*1024), // 1MB
		grpc.MaxConcurrentStreams(100),
	)

	// Register service
	pb.RegisterTunnelManagerServer(s.server, s)

	s.logger.Info("Starting gRPC server",
		zap.String("socketPath", s.socketPath))

	// Start serving (blocking)
	go func() {
		if err := s.server.Serve(listener); err != nil {
			s.logger.Error("gRPC server error", zap.Error(err))
		}
	}()

	return nil
}

// Stop gracefully stops the gRPC server
func (s *GRPCServer) Stop(ctx context.Context) error {
	s.logger.Info("Stopping gRPC server")

	if s.server != nil {
		// Graceful stop with context timeout
		stopped := make(chan struct{})
		go func() {
			s.server.GracefulStop()
			close(stopped)
		}()

		select {
		case <-stopped:
			s.logger.Info("gRPC server stopped gracefully")
		case <-ctx.Done():
			s.logger.Warn("gRPC server stop timeout, forcing shutdown")
			s.server.Stop()
		}
	}

	if s.listener != nil {
		s.listener.Close()
	}

	// Clean up socket file
	if err := os.RemoveAll(s.socketPath); err != nil {
		s.logger.Warn("Failed to remove socket file", zap.Error(err))
	}

	return nil
}

// EnsureTunnel implements the EnsureTunnel RPC method
func (s *GRPCServer) EnsureTunnel(ctx context.Context, req *pb.EnsureTunnelRequest) (*pb.EnsureTunnelResponse, error) {
	s.logger.Info("EnsureTunnel request",
		zap.String("volumeID", req.VolumeId),
		zap.String("nfsServer", req.NfsServer))

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volumeID is required")
	}
	if req.NfsServer == "" {
		return nil, status.Error(codes.InvalidArgument, "nfsServer is required")
	}

	// Check if tunnel already exists
	_, exists := s.manager.GetTunnel(req.VolumeId)
	created := !exists

	// Ensure tunnel exists
	tunnel, err := s.manager.EnsureTunnel(req.VolumeId, req.NfsServer)
	if err != nil {
		s.logger.Error("Failed to ensure tunnel",
			zap.String("volumeID", req.VolumeId),
			zap.Error(err))
		return nil, status.Errorf(codes.Internal, "failed to ensure tunnel: %v", err)
	}

	return &pb.EnsureTunnelResponse{
		Tunnel:  tunnelToProto(tunnel),
		Created: created,
	}, nil
}

// RemoveTunnel implements the RemoveTunnel RPC method
func (s *GRPCServer) RemoveTunnel(ctx context.Context, req *pb.RemoveTunnelRequest) (*pb.RemoveTunnelResponse, error) {
	s.logger.Info("RemoveTunnel request",
		zap.String("volumeID", req.VolumeId))

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volumeID is required")
	}

	// Get tunnel before removal to check if it exists
	_, exists := s.manager.GetTunnel(req.VolumeId)
	if !exists {
		return nil, status.Error(codes.NotFound, "tunnel not found")
	}

	// Remove tunnel
	if err := s.manager.RemoveTunnel(req.VolumeId); err != nil {
		s.logger.Error("Failed to remove tunnel",
			zap.String("volumeID", req.VolumeId),
			zap.Error(err))
		return nil, status.Errorf(codes.Internal, "failed to remove tunnel: %v", err)
	}

	// Check if tunnel was completely removed
	_, stillExists := s.manager.GetTunnel(req.VolumeId)
	removed := !stillExists

	newRefCount := int32(0)
	if stillExists {
		tunnel, _ := s.manager.GetTunnel(req.VolumeId)
		newRefCount = int32(tunnel.RefCount)
	}

	return &pb.RemoveTunnelResponse{
		Removed:  removed,
		RefCount: newRefCount,
	}, nil
}

// GetTunnel implements the GetTunnel RPC method
func (s *GRPCServer) GetTunnel(ctx context.Context, req *pb.GetTunnelRequest) (*pb.GetTunnelResponse, error) {
	s.logger.Debug("GetTunnel request",
		zap.String("volumeID", req.VolumeId))

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volumeID is required")
	}

	tunnel, exists := s.manager.GetTunnel(req.VolumeId)
	if !exists {
		return &pb.GetTunnelResponse{
			Tunnel: nil,
			Found:  false,
		}, nil
	}

	return &pb.GetTunnelResponse{
		Tunnel: tunnelToProto(tunnel),
		Found:  true,
	}, nil
}

// ListTunnels implements the ListTunnels RPC method
func (s *GRPCServer) ListTunnels(ctx context.Context, req *pb.ListTunnelsRequest) (*pb.ListTunnelsResponse, error) {
	s.logger.Debug("ListTunnels request")

	s.manager.mu.RLock()
	defer s.manager.mu.RUnlock()

	tunnels := make([]*pb.TunnelInfo, 0, len(s.manager.tunnels))
	for _, tunnel := range s.manager.tunnels {
		tunnels = append(tunnels, tunnelToProto(tunnel))
	}

	return &pb.ListTunnelsResponse{
		Tunnels:    tunnels,
		TotalCount: int32(len(tunnels)),
	}, nil
}

// Health implements the Health RPC method
func (s *GRPCServer) Health(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	s.logger.Debug("Health check request")

	s.manager.mu.RLock()
	defer s.manager.mu.RUnlock()

	activeTunnels := 0
	failedTunnels := 0

	for _, tunnel := range s.manager.tunnels {
		if tunnel.State == StateRunning {
			activeTunnels++
		} else if tunnel.State == StateFailed {
			failedTunnels++
		}
	}

	healthStatus := pb.HealthStatus_HEALTHY
	message := "Service is healthy"

	if failedTunnels > 0 {
		healthStatus = pb.HealthStatus_DEGRADED
		message = fmt.Sprintf("Service degraded: %d failed tunnels", failedTunnels)
	}

	if activeTunnels == 0 && len(s.manager.tunnels) > 0 {
		healthStatus = pb.HealthStatus_UNHEALTHY
		message = "Service unhealthy: no active tunnels"
	}

	return &pb.HealthResponse{
		Status:        healthStatus,
		Message:       message,
		ActiveTunnels: int32(activeTunnels),
		FailedTunnels: int32(failedTunnels),
	}, nil
}

// tunnelToProto converts a Tunnel to protobuf TunnelInfo
func tunnelToProto(t *Tunnel) *pb.TunnelInfo {
	if t == nil {
		return nil
	}

	return &pb.TunnelInfo{
		VolumeId:     t.VolumeID,
		RemoteAddr:   t.RemoteAddr,
		LocalPort:    int32(t.LocalPort),
		State:        string(t.State),
		RefCount:     int32(t.RefCount),
		RestartCount: int32(t.RestartCount),
		LastHealthy:  t.LastHealthy.Unix(),
		ConfigPath:   t.ConfigPath,
	}
}

// Made with Bob
