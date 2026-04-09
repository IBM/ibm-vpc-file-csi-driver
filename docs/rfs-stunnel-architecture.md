# RFS Profile with Stunnel Architecture

## Overview

This document describes the production-ready implementation of dynamic per-volume Stunnel tunnels for IBM VPC File Storage RFS (Remote File Storage) profile volumes with Encryption in Transit (EIT).

The current implementation uses a **tunnel-manager sidecar container inside the CSI node DaemonSet pod**. This decouples Stunnel lifecycle from the CSI node driver container and reduces the risk that a CSI driver container restart tears down active encrypted tunnels for mounted shares.

## Architecture

### Components

1. **Tunnel Manager Sidecar** (`pkg/tunnel/manager.go`, `pkg/tunnel/http_service.go`)
   - Runs as a sidecar container in the CSI node pod on every node
   - Manages lifecycle of Stunnel processes
   - Handles dynamic port allocation
   - Monitors tunnel health and performs automatic recovery
   - Persists tunnel metadata for recovery
   - Exposes a node-local Unix socket API to the CSI node server container

2. **CSI Node Server** (`pkg/ibmcsidriver/node.go`)
   - Detects RFS + EIT + Stunnel mount requests
   - Calls the sidecar tunnel-manager API over Unix socket
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
│                     Tunnel Manager Sidecar                            │
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
   - CSI Node Server calls the sidecar tunnel-manager API on the same node
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

Configure the tunnel-manager behavior via environment variables in the tunnel-manager sidecar container.

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

1. **Tunnel-manager Sidecar**
   - Runs in the same pod as the CSI node plugin container
   - Uses the shared node-local host paths for tunnel endpoints
   - Exposes a Unix socket in the existing host plugin path

2. **Stunnel Configuration**
   - Managed by the tunnel-manager sidecar
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
   - CSI node driver container restart does not directly own tunnel lifecycle
   - Standard NFS4 mounting on host
   - Better container-level isolation for encrypted mounts

### Configuration File Security

- Configuration and metadata files stored in `/etc/stunnel`
- File permissions: 0600 (owner read/write only)
- Automatic cleanup on final volume unmount
- Metadata is reused for best-effort recovery after tunnel-manager sidecar restart

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

## Scalability Limitations

### Sidecar Approach Constraints

The tunnel-manager sidecar architecture, while providing better isolation and reliability, has inherent scalability limitations:

#### 1. **Per-Node Process Limits**

**Limitation**: Each RFS volume with EIT requires a dedicated Stunnel process on the node
- **Impact**: High volume density can exhaust node process limits
- **Typical Limit**: Linux default `pid_max` is 32,768 processes per node
- **Practical Limit**: ~1,000-2,000 RFS volumes per node before hitting resource constraints

**Mitigation**:
- Monitor process count: `ps aux | wc -l`
- Increase `pid_max` if needed: `sysctl kernel.pid_max=65536`
- Use node selectors to distribute RFS workloads across nodes
- Consider connection pooling (future enhancement)

#### 2. **Port Exhaustion**

**Limitation**: Default port range supports 10,000 concurrent tunnels per node
- **Impact**: Cannot create more tunnels once port range is exhausted
- **Default Range**: 20000-30000 (10,000 ports)

**Mitigation**:
- Increase port range via `STUNNEL_BASE_PORT` and `STUNNEL_PORT_RANGE`
- Monitor port usage: `netstat -tuln | grep -c "127.0.0.1:2[0-9]"`
- Use multiple nodes for high-density workloads

#### 3. **Memory Overhead**

**Limitation**: Linear memory growth with tunnel count
- **Per-Tunnel Memory**: ~10-20 MB per Stunnel process
- **Example**: 1,000 tunnels = ~10-20 GB memory overhead
- **Impact**: Can exhaust node memory on high-density nodes

**Mitigation**:
- Set appropriate memory limits on tunnel-manager container
- Monitor memory usage: `kubectl top pod -n kube-system`
- Use memory-optimized node types for high-density scenarios
- Implement pod eviction policies based on memory pressure

#### 4. **File Descriptor Limits**

**Limitation**: Each tunnel consumes file descriptors for sockets and config files
- **Per-Tunnel FDs**: ~5-10 file descriptors
- **System Limit**: Default `ulimit -n` is typically 1024-65536
- **Impact**: Can hit FD limits with hundreds of tunnels

**Mitigation**:
- Increase FD limits in container: `ulimit -n 65536`
- Monitor FD usage: `lsof -p <tunnel-manager-pid> | wc -l`
- Set appropriate limits in DaemonSet spec:
  ```yaml
  resources:
    limits:
      nofile: 65536
  ```

#### 5. **Health Check Overhead**

**Limitation**: Health checks scale linearly with tunnel count
- **Default Interval**: 30 seconds per tunnel
- **Impact**: With 1,000 tunnels, ~33 health checks per second
- **CPU Impact**: Can consume significant CPU on high-density nodes

**Mitigation**:
- Increase health check interval for high-density scenarios
- Implement adaptive health checking (check more frequently on failure)
- Consider batch health checking (future enhancement)

#### 6. **Metadata File I/O**

**Limitation**: Each tunnel maintains a metadata file for recovery
- **Per-Tunnel Files**: 2 files (config + metadata) in `/etc/stunnel`
- **I/O Impact**: Frequent writes during tunnel lifecycle operations
- **Impact**: Can cause I/O contention on high-density nodes

**Mitigation**:
- Use fast local storage (SSD/NVMe) for `/etc/stunnel`
- Implement write batching for metadata updates (future enhancement)
- Monitor I/O wait: `iostat -x 1`

#### 7. **Recovery Time After Node Reboot**

**Limitation**: Recovery time increases with tunnel count
- **Recovery Process**: Validates each tunnel against `/proc/mounts`
- **Time Complexity**: O(n) where n = number of tunnels
- **Impact**: With 1,000 tunnels, recovery can take 30-60 seconds

**Mitigation**:
- Implement parallel recovery (future enhancement)
- Use faster storage for metadata files
- Monitor recovery time in logs

### Architectural Alternatives

For extremely high-density scenarios (>1,000 volumes per node), consider:

#### 1. **Connection Pooling**
- Share tunnels between volumes from the same NFS server
- Reduces process count and resource usage
- Requires careful refcount management
- **Trade-off**: Increased complexity, potential security concerns

#### 2. **Proxy-Based Architecture**
- Single proxy process handling multiple connections
- Better resource efficiency at scale
- Requires significant architectural changes
- **Trade-off**: Single point of failure, increased complexity

#### 3. **Kernel-Level TLS**
- Use kernel TLS (kTLS) for NFS encryption
- Eliminates userspace tunnel processes
- Requires kernel support and NFS server compatibility
- **Trade-off**: Limited availability, platform-specific

### Recommended Deployment Patterns

#### Small-Scale Deployments (<100 volumes per node)
- ✅ Use default configuration
- ✅ Standard health check interval (30s)
- ✅ No special tuning required

#### Medium-Scale Deployments (100-500 volumes per node)
- ⚠️ Monitor resource usage
- ⚠️ Increase health check interval to 60s
- ⚠️ Set appropriate memory limits
- ⚠️ Use node selectors for workload distribution

#### Large-Scale Deployments (500-1000 volumes per node)
- 🔴 Increase `pid_max` and FD limits
- 🔴 Use fast local storage for metadata
- 🔴 Increase health check interval to 120s
- 🔴 Monitor recovery time after reboots
- 🔴 Consider dedicated nodes for RFS workloads

#### Very Large-Scale Deployments (>1000 volumes per node)
- 🚫 **Not recommended** with current sidecar architecture
- 🚫 Consider architectural alternatives (connection pooling, proxy)
- 🚫 Distribute workloads across multiple nodes
- 🚫 Evaluate if RFS profile is appropriate for use case

### Monitoring Recommendations

Monitor these metrics to detect scalability issues:

```bash
# Process count
ps aux | grep stunnel | wc -l

# Port usage
netstat -tuln | grep "127.0.0.1:2[0-9]" | wc -l

# Memory usage
kubectl top pod -n kube-system -l app=ibm-vpc-file-csi-driver

# File descriptor usage
lsof -p $(pgrep tunnel-manager) | wc -l

# Recovery time (from logs)
kubectl logs -n kube-system <csi-pod> -c tunnel-manager | grep "Tunnel recovery completed"
```

### Summary

The tunnel-manager sidecar approach provides:
- ✅ **Excellent reliability** (survives CSI driver restarts)
- ✅ **Good isolation** (separate process per volume)
- ✅ **Proven scalability** up to ~500 volumes per node
- ⚠️ **Limited scalability** beyond 1,000 volumes per node
- 🔴 **Not suitable** for extreme high-density scenarios (>2,000 volumes per node)

For most production workloads, the current architecture is appropriate and well-tested.

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