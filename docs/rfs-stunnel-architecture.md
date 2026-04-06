# RFS Profile with Stunnel Architecture

## Overview

This document describes the production-ready implementation of dynamic per-volume Stunnel tunnels for IBM VPC File Storage RFS (Remote File Storage) profile volumes with Encryption in Transit (EIT).

The current implementation uses a **separate node-local tunnel-manager DaemonSet**. This decouples Stunnel lifecycle from the CSI node plugin pod and reduces the risk that a CSI node pod restart tears down active encrypted tunnels for mounted shares.

## Architecture

### Components

1. **Tunnel Manager DaemonSet** (`pkg/tunnel/manager.go`, `pkg/tunnel/http_service.go`)
   - Runs as a separate pod on every node
   - Manages lifecycle of Stunnel processes
   - Handles dynamic port allocation
   - Monitors tunnel health and performs automatic recovery
   - Persists tunnel metadata for recovery
   - Exposes a node-local Unix socket API to the CSI node server

2. **CSI Node Server** (`pkg/ibmcsidriver/node.go`)
   - Detects RFS + EIT + Stunnel mount requests
   - Calls the node-local tunnel-manager API over Unix socket
   - Mounts the share through the returned local port
   - Cleans up tunnels during volume unmount

3. **Stunnel Process** (per volume)
   - Runs under tunnel-manager control
   - Provides TLS encryption for NFS traffic
   - Verifies server certificates against system CA bundle

### Flow Diagram

```
┌──────────────────────────────────────────────────────────────────────┐
│                             Pod with PVC                              │
└─────────────────────────────┬────────────────────────────────────────┘
                              │
                              │ Mount Request
                              ▼
┌──────────────────────────────────────────────────────────────────────┐
│                         CSI Node Server                                │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │ NodePublishVolume                                               │  │
│  │  1. Detect RFS profile + EIT enabled                            │  │
│  │  2. Parse NFS source into <nfsServer> and <exportPath>          │  │
│  │  3. Call tunnel-manager API over Unix socket                    │  │
│  │  4. Receive local tunnel port                                   │  │
│  │  5. Mount 127.0.0.1:/<exportPath> with port=<localPort>         │  │
│  └────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────┬────────────────────────────────────────┘
                              │
                              │ Unix domain socket
                              │ /var/lib/kubelet/plugins/vpc.file.csi.ibm.io/tunnel-manager.sock
                              ▼
┌──────────────────────────────────────────────────────────────────────┐
│                    Tunnel Manager DaemonSet                           │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │ EnsureTunnel                                                     │  │
│  │  1. Check if tunnel exists and is healthy                       │  │
│  │  2. Allocate port (hash-based with fallback)                    │  │
│  │  3. Generate Stunnel configuration in /etc/stunnel              │  │
│  │  4. Start Stunnel process                                       │  │
│  │  5. Wait for tunnel to be ready                                 │  │
│  │  6. Persist metadata and start health monitoring                │  │
│  └────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────┬────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────────────┐
│                     Stunnel Process (per volume)                      │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │ Configuration:                                                  │  │
│  │  - client = yes                                                 │  │
│  │  - accept = 127.0.0.1:<allocated_port>                         │  │
│  │  - connect = <nfs_server>:20049                                │  │
│  │  - cafile = <configured CA file>                               │  │
│  │  - checkHost = <env>.is-share.appdomain.cloud                  │  │
│  │  - verify = 1                                                  │  │
│  └────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────┬────────────────────────────────────────┘
                              │
                              │ TLS Connection
                              ▼
┌──────────────────────────────────────────────────────────────────────┐
│                   IBM VPC RFS NFS Server (port 20049)                 │
└──────────────────────────────────────────────────────────────────────┘
```

### Mount Flow

1. **Volume Mount Request**
   - User creates PVC with RFS storage class
   - Kubernetes schedules pod and requests volume mount

2. **Tunnel Establishment**
   - CSI Node Server detects RFS profile with EIT enabled
   - NFS source is parsed to extract server address and export path
   - CSI Node Server calls the tunnel-manager API on the same node
   - Tunnel Manager allocates a unique port (20000-30000 range)
   - Stunnel configuration is generated with proper TLS settings in `/etc/stunnel`
   - Stunnel process starts and connects to the remote NFS server

3. **NFS4 Mount**
   - Mount source is rewritten to use local tunnel endpoint: `127.0.0.1:/<export_path>`
   - NFS4 mount is performed with specific options: `vers=4.1,proto=tcp,port=<tunnel_port>`
   - Mount is executed on the host with access to host CA certificates
   - All NFS traffic is encrypted via TLS through the Stunnel tunnel

4. **Health Monitoring**
   - Tunnel Manager periodically checks tunnel health (default: 30s)
   - Automatic restart on failure with exponential backoff
   - Maximum 3 restart attempts before giving up

### Unmount Flow

1. **Volume Unmount Request**
   - Pod is deleted or volume is unmounted

2. **NFS Unmount**
   - Standard NFS unmount is performed

3. **Tunnel Cleanup**
   - CSI node server calls `RemoveTunnel` after successful unmount
   - Tunnel Manager decrements reference count
   - Stunnel process is removed only when refcount reaches zero
   - Configuration and metadata files are removed
   - Port is released for reuse

## Configuration

### Environment Variables

Configure the tunnel-manager behavior via environment variables in the tunnel-manager DaemonSet.

| Variable | Default | Description |
|----------|---------|-------------|
| `TUNNEL_MANAGER_SOCKET` | `/var/lib/kubelet/plugins/vpc.file.csi.ibm.io/tunnel-manager.sock` | Unix domain socket used by CSI node to call tunnel-manager |
| `STUNNEL_BASE_PORT` | 20000 | Starting port for tunnel allocation |
| `STUNNEL_PORT_RANGE` | 10000 | Number of ports available for allocation |
| `STUNNEL_CONFIG_DIR` | /etc/stunnel | Directory for Stunnel configurations and metadata |
| `STUNNEL_CA_FILE` | /host-certs/tls-ca-bundle.pem | CA bundle path used for certificate verification |
| `STUNNEL_NFS_PORT` | 20049 | NFS port on RFS servers |
| `STUNNEL_ENVIRONMENT` | production | Environment for hostname verification (staging/production) |
| `STUNNEL_HEALTH_CHECK_INTERVAL` | 30s | Interval for tunnel health checks |

The CSI node DaemonSet uses:
- `TUNNEL_MANAGER_SOCKET=/var/lib/kubelet/plugins/vpc.file.csi.ibm.io/tunnel-manager.sock`

### Storage Class Parameters

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ibmc-vpc-file-rfs-stunnel
provisioner: vpc.file.csi.ibm.io
parameters:
  profile: "rfs"              # Required: Use RFS profile
  isEITEnabled: "true"        # Required: Enable encryption in transit
  throughput: "1000"          # Required: Bandwidth in MB/s (25-8192)
  isENIEnabled: "true"        # Recommended: Use ENI for better performance
  # ... other parameters
```

## Port Allocation Strategy

### Hash-Based Allocation

1. **Primary Method**: Hash volume ID to determine preferred port
   - Consistent port assignment for same volume ID
   - Reduces port conflicts on restart

2. **Fallback Method**: Linear search for available port
   - Used when preferred port is unavailable
   - Ensures volume can always get a port

3. **Port Availability Check**
   - Attempts to bind to port before allocation
   - Prevents conflicts with other services

### Port Range

- Default range: 20000-30000 (10,000 ports)
- Supports up to 10,000 concurrent RFS volumes per node
- Configurable via `STUNNEL_BASE_PORT` and `STUNNEL_PORT_RANGE`

## Security

### TLS Certificate Verification

- Uses configured CA bundle path from the tunnel-manager environment
- Verifies server certificate against trusted CAs
- Validates hostname matches `<env>.is-share.appdomain.cloud`
- Verification level set to 1 (verify certificate chain)

### Host Network and NFS4 Mounting

The implementation is designed to work with host network access and node-local process isolation:

1. **Tunnel-manager DaemonSet**
   - Runs independently from the CSI node plugin pod
   - Uses host network access for node-local tunnel endpoints
   - Exposes a Unix socket in the existing host plugin path

2. **Stunnel Configuration**
   - Managed by the tunnel-manager DaemonSet
   - Listens on localhost (127.0.0.1) with allocated port
   - Stores configuration and metadata in `/etc/stunnel`

3. **NFS4 Mount Command**
   ```bash
   sudo mount -t nfs4 \
     -o vers=4.1,proto=tcp,port=<tunnel_port> \
     127.0.0.1:/<export_path> \
     <target_path>
   ```

4. **Mount Options**
   - `vers=4.1`: Use NFS version 4.1 protocol
   - `proto=tcp`: Use TCP protocol
   - `port=<tunnel_port>`: Connect to Stunnel's local port
   - Mount source format: `127.0.0.1:/<export_path>`

5. **Benefits**
   - CSI node pod restart does not directly own tunnel lifecycle
   - Standard NFS4 mounting on host
   - Better node-local process isolation for encrypted mounts

### Configuration File Security

- Configuration and metadata files stored in `/etc/stunnel`
- File permissions: 0600 (owner read/write only)
- Automatic cleanup on final volume unmount
- Metadata is reused for best-effort recovery after tunnel-manager restart

## Health Monitoring

### Health Check Process

1. **Periodic Checks** (default: every 30 seconds)
   - Verify Stunnel process is running
   - Test TCP connection to local port
   - Update last healthy timestamp

2. **Failure Detection**
   - Process crash detection via process monitoring
   - Port unavailability detection via health checks

3. **Automatic Recovery**
   - Restart Stunnel process on failure
   - Exponential backoff: 2s, 4s, 6s
   - Maximum 3 restart attempts
   - Detailed logging for troubleshooting

### Process Monitoring

- Each Stunnel process runs in foreground mode
- Go routine monitors process exit
- Distinguishes between intentional stop and crash
- Automatic restart on unexpected termination

## Logging

### Log Levels

- **Info**: Normal operations (tunnel creation, removal, health checks)
- **Warn**: Recoverable issues (health check failures, restart attempts)
- **Error**: Unrecoverable errors (tunnel creation failures, restart limit exceeded)

### Log Fields

All log entries include:
- `volumeID`: Unique volume identifier
- `nfsServer`: Remote NFS server address
- `port`: Allocated local port
- `state`: Current tunnel state
- Additional context-specific fields

### Example Logs

```
INFO  Allocated port for tunnel  volumeID=vol-123 nfsServer=10.0.0.1 port=20042
INFO  Stunnel process started  volumeID=vol-123 pid=12345
INFO  Tunnel created successfully  volumeID=vol-123 nfsServer=10.0.0.1 port=20042
WARN  Tunnel health check failed, attempting restart  volumeID=vol-123
INFO  Tunnel restarted successfully  volumeID=vol-123
INFO  Removing tunnel  volumeID=vol-123
INFO  Tunnel removed successfully  volumeID=vol-123
```

## Troubleshooting

### Common Issues

#### 1. Tunnel Creation Fails

**Symptoms**: Volume mount fails with tunnel creation error

**Possible Causes**:
- No available ports in range
- Stunnel binary not found
- CA bundle not accessible
- Network connectivity issues

**Solutions**:
- Check port range configuration
- Verify Stunnel is installed in container
- Verify CA bundle path
- Check network connectivity to NFS server

#### 2. Tunnel Health Check Failures

**Symptoms**: Frequent tunnel restarts in logs

**Possible Causes**:
- Network instability
- NFS server issues
- Resource constraints

**Solutions**:
- Check network connectivity
- Verify NFS server health
- Check node resource usage
- Increase health check interval

#### 3. Port Conflicts

**Symptoms**: Port allocation failures

**Possible Causes**:
- Port range too small
- Other services using ports in range

**Solutions**:
- Increase port range
- Change base port to avoid conflicts
- Check for other services on node

### Debug Commands

```bash
# Check Stunnel processes
ps aux | grep stunnel

# Check tunnel configurations
ls -la /var/lib/ibm-csi-driver/stunnel/

# Check port usage
netstat -tuln | grep 2[0-9][0-9][0-9][0-9]

# Check CSI driver logs
kubectl logs -n kube-system <csi-node-pod> -c ibm-vpc-file-csi-driver

# Check tunnel manager stats (if exposed)
# Access via debug endpoint or logs
```

## Performance Considerations

### Resource Usage

- **Memory**: ~10-20 MB per Stunnel process
- **CPU**: Minimal (<1% per tunnel under normal load)
- **Network**: TLS overhead ~5-10% compared to unencrypted

### Scalability

- Supports up to 10,000 concurrent tunnels per node (default port range)
- Linear resource scaling with number of volumes
- No performance degradation with multiple tunnels

### Optimization Tips

1. **Port Range**: Size appropriately for expected volume count
2. **Health Check Interval**: Balance between responsiveness and overhead
3. **Resource Limits**: Set appropriate limits on node DaemonSet

## Future Enhancements

Potential improvements for future versions:

1. **Metrics Export**: Prometheus metrics for tunnel operations
2. **Dynamic Port Range**: Automatically expand port range if needed
3. **Connection Pooling**: Share tunnels for volumes from same server
4. **Certificate Rotation**: Support for certificate updates without restart
5. **Advanced Health Checks**: Application-level NFS health checks

## References

- [Stunnel Documentation](https://www.stunnel.org/docs.html)
- [IBM VPC File Storage](https://cloud.ibm.com/docs/vpc?topic=vpc-file-storage-vpc-about)
- [CSI Specification](https://github.com/container-storage-interface/spec)