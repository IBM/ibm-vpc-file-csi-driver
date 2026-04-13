package tunnel

import (
	"context"
	"fmt"
	"net"

	pb "github.com/IBM/ibm-vpc-file-csi-driver/pkg/tunnel/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GRPCClient implements the Service interface using gRPC
type GRPCClient struct {
	socketPath  string
	conn        *grpc.ClientConn
	client      pb.TunnelManagerClient
	logger      *zap.Logger
	retryConfig RetryConfig
}

// NewGRPCClient creates a new gRPC client for tunnel management
func NewGRPCClient(socketPath string, logger *zap.Logger) (*GRPCClient, error) {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	// Use default retry configuration
	retryConfig := DefaultRetryConfig()

	// Create custom dialer for Unix socket
	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
		return net.Dial("unix", addr)
	}

	// Connect to Unix socket with non-blocking dial
	// This allows the CSI driver to start even if tunnel-manager is temporarily unavailable
	// The connection will be established in the background and retried automatically
	conn, err := grpc.Dial(
		socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
		// Removed grpc.WithBlock() to make connection non-blocking
		// This prevents CSI driver startup from being blocked if tunnel-manager is down
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to tunnel-manager at %s: %w", socketPath, err)
	}

	client := pb.NewTunnelManagerClient(conn)

	return &GRPCClient{
		socketPath:  socketPath,
		conn:        conn,
		client:      client,
		logger:      logger,
		retryConfig: retryConfig,
	}, nil
}

// Close closes the gRPC connection
func (c *GRPCClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// EnsureTunnel creates or reuses a tunnel for the given volume
// Retries automatically on transient failures (e.g., tunnel-manager temporarily unavailable)
func (c *GRPCClient) EnsureTunnel(ctx context.Context, volumeID, nfsServer string) (*TunnelInfo, error) {
	c.logger.Debug("EnsureTunnel gRPC request",
		zap.String("volumeID", volumeID),
		zap.String("nfsServer", nfsServer))

	var tunnelInfo *TunnelInfo
	var created bool

	// Retry the RPC call with exponential backoff
	err := retryWithBackoff(ctx, c.logger, c.retryConfig, "EnsureTunnel", func() error {
		req := &pb.EnsureTunnelRequest{
			VolumeId:  volumeID,
			NfsServer: nfsServer,
		}

		resp, err := c.client.EnsureTunnel(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to ensure tunnel: %w", err)
		}

		if resp.Tunnel == nil {
			return fmt.Errorf("tunnel-manager returned nil tunnel")
		}

		tunnelInfo = protoToTunnelInfo(resp.Tunnel)
		created = resp.Created
		return nil
	})

	if err != nil {
		c.logger.Error("EnsureTunnel gRPC failed after retries",
			zap.String("volumeID", volumeID),
			zap.Error(err))
		return nil, err
	}

	c.logger.Info("EnsureTunnel gRPC succeeded",
		zap.String("volumeID", volumeID),
		zap.Int("localPort", tunnelInfo.LocalPort),
		zap.Bool("created", created))

	return tunnelInfo, nil
}

// RemoveTunnel decrements refcount and removes tunnel if refcount reaches zero
// Retries automatically on transient failures (e.g., tunnel-manager temporarily unavailable)
func (c *GRPCClient) RemoveTunnel(ctx context.Context, volumeID string) error {
	c.logger.Debug("RemoveTunnel gRPC request",
		zap.String("volumeID", volumeID))

	var removed bool
	var refCount int32

	// Retry the RPC call with exponential backoff
	err := retryWithBackoff(ctx, c.logger, c.retryConfig, "RemoveTunnel", func() error {
		req := &pb.RemoveTunnelRequest{
			VolumeId: volumeID,
		}

		resp, err := c.client.RemoveTunnel(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to remove tunnel: %w", err)
		}

		removed = resp.Removed
		refCount = resp.RefCount
		return nil
	})

	if err != nil {
		c.logger.Error("RemoveTunnel gRPC failed after retries",
			zap.String("volumeID", volumeID),
			zap.Error(err))
		return err
	}

	c.logger.Info("RemoveTunnel gRPC succeeded",
		zap.String("volumeID", volumeID),
		zap.Bool("removed", removed),
		zap.Int32("refCount", refCount))

	return nil
}

// GetTunnel retrieves information about a specific tunnel
func (c *GRPCClient) GetTunnel(ctx context.Context, volumeID string) (*TunnelInfo, bool, error) {
	c.logger.Debug("GetTunnel gRPC request",
		zap.String("volumeID", volumeID))

	req := &pb.GetTunnelRequest{
		VolumeId: volumeID,
	}

	resp, err := c.client.GetTunnel(ctx, req)
	if err != nil {
		c.logger.Error("GetTunnel gRPC failed",
			zap.String("volumeID", volumeID),
			zap.Error(err))
		return nil, false, fmt.Errorf("failed to get tunnel: %w", err)
	}

	if !resp.Found || resp.Tunnel == nil {
		return nil, false, nil
	}

	tunnelInfo := protoToTunnelInfo(resp.Tunnel)
	return tunnelInfo, true, nil
}

// ListTunnels returns all active tunnels
func (c *GRPCClient) ListTunnels(ctx context.Context) ([]*TunnelInfo, error) {
	c.logger.Debug("ListTunnels gRPC request")

	req := &pb.ListTunnelsRequest{}

	resp, err := c.client.ListTunnels(ctx, req)
	if err != nil {
		c.logger.Error("ListTunnels gRPC failed", zap.Error(err))
		return nil, fmt.Errorf("failed to list tunnels: %w", err)
	}

	tunnels := make([]*TunnelInfo, 0, len(resp.Tunnels))
	for _, pbTunnel := range resp.Tunnels {
		tunnels = append(tunnels, protoToTunnelInfo(pbTunnel))
	}

	c.logger.Debug("ListTunnels gRPC succeeded",
		zap.Int("count", len(tunnels)))

	return tunnels, nil
}

// Health checks the health of the tunnel manager service
func (c *GRPCClient) Health(ctx context.Context) error {
	c.logger.Debug("Health gRPC request")

	req := &pb.HealthRequest{}

	resp, err := c.client.Health(ctx, req)
	if err != nil {
		c.logger.Error("Health gRPC failed", zap.Error(err))
		return fmt.Errorf("health check failed: %w", err)
	}

	if resp.Status != pb.HealthStatus_HEALTHY {
		c.logger.Warn("Tunnel manager health degraded",
			zap.String("status", resp.Status.String()),
			zap.String("message", resp.Message),
			zap.Int32("activeTunnels", resp.ActiveTunnels),
			zap.Int32("failedTunnels", resp.FailedTunnels))
		return fmt.Errorf("tunnel manager unhealthy: %s", resp.Message)
	}

	c.logger.Debug("Health gRPC succeeded",
		zap.String("status", resp.Status.String()),
		zap.Int32("activeTunnels", resp.ActiveTunnels))

	return nil
}

// protoToTunnelInfo converts a protobuf TunnelInfo to a TunnelInfo
func protoToTunnelInfo(pb *pb.TunnelInfo) *TunnelInfo {
	if pb == nil {
		return nil
	}

	return &TunnelInfo{
		VolumeID:   pb.VolumeId,
		RemoteAddr: pb.RemoteAddr,
		LocalPort:  int(pb.LocalPort),
		State:      pb.State,
		RefCount:   int(pb.RefCount),
	}
}

// Made with Bob
