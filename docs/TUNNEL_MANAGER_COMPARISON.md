# Tunnel Manager Comparison: CSI Driver vs Denali

## Overview

This document compares two tunnel management approaches for IBM VPC File CSI Driver with encryption in transit:

1. **CSI Driver-Owned Tunnel Manager** (ARCHITECTURE.md) - gRPC-based with reference counting
2. **Denali-Owned Tunnel Manager** (STUNNEL_TUNNEL_MANAGEMENT_ARCHITECTURE.md) - Simple manager with mount table query

---

## Architecture Comparison

### CSI Driver-Owned Tunnel Manager

```
┌─────────────────────────────────────────────────────────────┐
│                    CSI Node Pod                             │
│                                                             │
│  ┌──────────────────┐         ┌──────────────────────────┐ │
│  │  CSI Driver      │  gRPC   │  Tunnel Manager          │ │
│  │  Container       │◄───────►│  Container               │ │
│  │                  │         │                          │ │
│  │  - Mount/Unmount │         │  - Reference Counting    │ │
│  │  - gRPC Client   │         │  - Metadata Persistence  │ │
│  │                  │         │  - Health Checks         │ │
│  │                  │         │  - Stunnel Management    │ │
│  └──────────────────┘         └──────────┬───────────────┘ │
│                                           │                 │
│                                           v                 │
│                                ┌──────────────────────────┐ │
│                                │  Stunnel Process         │ │
│                                │  (1 per file share)      │ │
│                                └──────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘

Ownership: CSI Driver controls tunnel lifecycle via gRPC
```

### Denali-Owned Tunnel Manager

```
┌─────────────────────────────────────────────────────────────┐
│                    CSI Node Pod                             │
│  (shareProcessNamespace: true)                              │
│                                                             │
│  ┌──────────────────┐         ┌──────────────────────────┐ │
│  │  CSI Driver      │ SIGHUP  │  denali-stunnel          │ │
│  │  Container       │────────►│  Container               │ │
│  │                  │         │                          │ │
│  │  - Mount/Unmount │         │  - Stunnel Process       │ │
│  │  - SimpleManager │         │  - Config Polling        │ │
│  │  - Config Write  │         │  - Auto-reload           │ │
│  │  - Mount Query   │         │                          │ │
│  └──────────────────┘         └──────────┬───────────────┘ │
│                                           │                 │
│                                           v                 │
│                                ┌──────────────────────────┐ │
│                                │  Stunnel Listeners       │ │
│                                │  (1 per file share)      │ │
│                                └──────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘

Ownership: Denali container owns stunnel, CSI driver writes configs
```

---

## Key Differences Summary

| Aspect | CSI Driver-Owned | Denali-Owned |
|--------|------------------|--------------|
| **Tunnel sharing** | Reference counting | Mount table query + NFS4 multiplexing |
| **Stunnel architecture** | 1 stunnel process per file share | 1 stunnel process with multiple listeners (1 per share) |
| **Processes per share** | 1 stunnel process (RefCount tracked) | 1 listener in shared stunnel process |
| **State persistence** | Metadata files (*.meta) | In-memory + EmptyDir configs |
| **Recovery mechanism** | Load metadata + verify mounts | Rebuild port allocation map (CSI driver only, stunnel auto-reads configs) |
| **Recovery purpose** | Restore tunnel state + RefCounts | Prevent port conflicts after CSI driver restart |
| **Deletion safety** | RefCount > 0 check | /proc/mounts port usage check |
| **Communication** | gRPC service | Direct SIGHUP signaling |
| **Complexity** | Higher (gRPC, metadata, health checks) | Lower (simple manager, file-based) |
| **Crash recovery** | Metadata-based (persistent) | Config-based (ephemeral) |
| **Best for** | Complex multi-tenant scenarios | Simpler deployments with NFS4 |

---

## Feature Comparison

| Feature | CSI Driver-Owned | Denali-Owned |
|---------|------------------|--------------|
| **Communication** | gRPC service | File-based + SIGHUP |
| **State Management** | Metadata files (*.meta) | In-memory map + EmptyDir |
| **Tunnel Sharing** | Reference counting | Mount table query + NFS4 multiplexing |
| **Processes per Share** | 1 stunnel (RefCount tracked) | 1 stunnel listener (mount tracked) |
| **Recovery** | Load metadata + verify mounts | Scan config files + rebuild port map |
| **Health Checks** | Active monitoring + auto-restart | Passive (denali polling) |
| **Deletion Safety** | RefCount > 0 check | /proc/mounts port usage check |
| **Complexity** | Higher (gRPC, metadata, health) | Lower (simple file-based) |
| **Crash Recovery** | Persistent (metadata files) | Ephemeral (EmptyDir configs) |

---

## Scalability

### CSI Driver-Owned
- **Max tunnels**: ~10,000 per node (port range limit)
- **Typical usage**: 10-100 tunnels per node
- **Resource per tunnel**: 5-10MB memory, minimal CPU **per process**
- **Scaling model**: Reference counting enables efficient sharing
- **Port allocation**: O(1) average, hash-based with bitmap
- **Processes**: **1 dedicated stunnel process per file share** (N shares = N processes)
- **Lines of code**: ~2000 LOC (gRPC service, metadata management, health checks)

### Denali-Owned
- **Max tunnels**: 10,000 ports (20000-29999), ~1000 practical
- **Per-node capacity**:
  - Small (4 CPU, 16GB): 500 tunnels, 2000 pods
  - Medium (8 CPU, 32GB): 1000 tunnels, 5000 pods
  - Large (16 CPU, 64GB): 2000 tunnels, 10,000 pods
- **Resource per tunnel**: 5-10MB memory **per listener** (shared process overhead)
- **Scaling model**: NFS4 multiplexing (multiple pods share TCP connection)
- **Cluster-wide**: Linear scaling, no central bottleneck
- **Processes**: **1 stunnel process with multiple listeners** (N shares = 1 process with N services)
- **Lines of code**: ~434 LOC (simple_manager.go - file-based config management)

---

## Performance

### CSI Driver-Owned
- **Metadata operations**: Async writes, batch recovery, minimal disk I/O
- **Port allocation**: O(1) average case
- **gRPC overhead**: Minimal (local Unix socket)
- **Resource usage**: No degradation with multiple tunnels
- **Optimization**: Hash-based port allocation, bitmap lookup

### Denali-Owned
- **Tunnel creation**: 15-55ms per tunnel, ~100 tunnels/second
- **Mount table query**: 2ms (100 mounts), 5ms (1000 mounts), 20ms (10,000 mounts)
- **NFS traffic**: 1-10 Gbps per tunnel, ~0.1ms TLS overhead
- **Mount/unmount**: ~1-5ms overhead per operation
- **SIGHUP signaling**: Immediate (< 1 second) vs 10-second polling fallback

---

## Certificate Management

### CSI Driver-Owned (Tunnel Manager Container)
- **TLS verification**: verify=2 (require and verify peer certificate)
- **CA bundle**: System CA bundle for validation
- **Management**: Certificates managed by IBM Cloud infrastructure
- **Encryption**: TLS 1.2+, strong cipher suites
- **Rotation**: Managed externally, stunnel auto-reloads
- **Certificate Expiry**:
  - **Outstanding Question**: How to handle certificate expiry in tunnel-manager image?
  - **Options**:
    - Release new tunnel-manager image before certificate expiry
    - Mount certificates via volume (allows runtime updates)
    - Use cert-manager for automatic rotation
  - **Current Status**: Needs decision on certificate lifecycle management

### Denali-Owned (denali-stunnel Container)
- **Storage**: /etc/tls (HostPath volume)
- **Encryption**: TLS 1.3 for NFS traffic
- **Sources**: Image build-time or runtime volume mount
- **Rotation**: Manual (rebuild image or update volume)
- **Certificate Expiry**:
  - **Outstanding Question**: How to handle certificate expiry in denali-stunnel image?
  - **Options**:
    - Release new denali-stunnel image before certificate expiry
    - Mount certificates via volume (allows runtime updates)
    - Use cert-manager for automatic rotation
  - **Current Status**: Needs decision on certificate lifecycle management
- **Note**: Certificate management section removed from current implementation

**Common Challenge**: Both approaches need a strategy for certificate expiry handling to avoid service disruption.

---

## Maintenance

### CSI Driver-Owned
- **Metadata persistence**: Files in /etc/stunnel/*.meta for recovery
- **Health checks**: Continuous monitoring with auto-restart
- **Recovery process**:
  1. Load metadata from persistent storage
  2. Verify actual mounts from /proc/mounts
  3. Restart tunnels with verified RefCounts
  4. Clean up stale metadata
- **Logging**: Structured logging (zap), DEBUG/INFO/WARN/ERROR levels
- **Monitoring metrics**: TotalTunnels, RunningTunnels, FailedTunnels, PortsAllocated

### Denali-Owned
- **State management**: In-memory allocatedPorts map, EmptyDir for configs
- **Recovery**: recoverExistingTunnels() scans /etc/stunnel/services/*.conf files on restart
  - Reads each config file to extract shareID and port number
  - Rebuilds allocatedPorts map (shareID → port mapping)
  - Restores in-memory state from config files
  - Example: r026-abc123.conf → allocatedPorts["r026-abc123"] = 20000
- **Mount table query**: /proc/mounts for safe tunnel deletion (never hangs)
- **Lifecycle scenarios**: 6 detailed scenarios documented:
  - Node reboot
  - CSI pod restart
  - CSI pod crash
  - Worker node replacement
  - Worker node upgrade
  - denali-stunnel container restart
- **Monitoring**: Tunnel count, port utilization, memory usage, mount latency, OOM kills
- **Alerts**: 
  - Critical: Port >90%, Memory >80%, OOM kills >0
  - Warning: Port >70%, Memory >60%, Tunnels >500

---

## Stability

### CSI Driver-Owned
- **Reference counting**: Prevents premature tunnel deletion
- **Failure recovery**:
  - **Stunnel crash**: Preserve RefCount, restart with same config
  - **Pod restart**: Load metadata, verify mounts, clean stale data
  - **NFS unreachable**: Keep tunnel running, auto-reconnect
- **Data safety**: No automatic cleanup when server down (preserve user data)
- **/proc/mounts usage**: Never hangs, accurate even when NFS down, works in degraded states
- **Health monitoring**: Active health checks detect and recover from failures

### Denali-Owned
- **Mount table query**: Checks /proc/mounts before tunnel deletion (safe removal)
- **NFS4 multiplexing**: Single TCP connection survives tunnel config changes
- **Failure recovery**:
  - **CSI pod crash**:
    - In-memory allocatedPorts map lost
    - On restart: recoverExistingTunnels() scans /etc/stunnel/services/*.conf
    - Rebuilds port map from config files
    - Tunnels auto-recreate on remount (1-3 min recovery)
  - **denali-stunnel crash**:
    - Configs preserved in EmptyDir
    - Stunnel reads all configs on restart
    - Auto-restore (1-2 sec)
  - **Node reboot**:
    - EmptyDir destroyed (fresh start)
    - Clean recovery, all mounts restored via new tunnel creation
- **EmptyDir behavior**: Survives container restart, destroyed on pod restart
- **SIGHUP fallback**: Polling every 10 seconds if signaling fails
- **No active health checks**: Relies on Kubernetes container restart policies

---

## Process Architecture per File Share

### CSI Driver-Owned
```
File Share A → 1 dedicated stunnel process → Multiple pods via RefCount
├─> Pod 1 mount (RefCount = 1)
├─> Pod 2 mount (RefCount = 2)
├─> Pod 3 mount (RefCount = 3)
└─> Single stunnel listener on port 20000
    └─> All pods connect to same port
    └─> Metadata file tracks RefCount
    └─> Process deleted when RefCount = 0

File Share B → 1 dedicated stunnel process → port 20001
File Share C → 1 dedicated stunnel process → port 20002

Total: 3 file shares = 3 separate stunnel processes
```

**Code Maintenance (CSI Driver-Owned):**
- **Lines of Code**: ~2000 LOC across multiple files
- **Components**: gRPC service, metadata persistence, health monitoring, process management
- **Complexity**: Higher - requires managing multiple process lifecycles

### Denali-Owned
```
Single stunnel process with multiple listeners (one per file share):

File Share A → Listener on port 20000 → Multiple pods via NFS4 multiplexing
├─> Pod 1 mount (kernel NFS client)
├─> Pod 2 mount (kernel NFS client)
├─> Pod 3 mount (kernel NFS client)
└─> Single TCP connection (NFS4 session)
    └─> All pods share one connection
    └─> /proc/mounts tracks active mounts
    └─> Listener removed when no mounts found

File Share B → Listener on port 20001 (same stunnel process)
File Share C → Listener on port 20002 (same stunnel process)

Total: 3 file shares = 1 stunnel process with 3 listeners/services
```

**Code Maintenance (Denali-Owned):**
- **Lines of Code**: ~434 LOC (pkg/stunnel/simple_manager.go)
- **Components**: Config file management, port allocation, SIGHUP signaling
- **Complexity**: Lower - stunnel handles process management, CSI driver only manages configs

**Key Insight**:
- **CSI Driver-Owned**: **1 stunnel process per file share** (multiple processes) - ~2000 LOC
- **Denali-Owned**: **1 stunnel process with multiple listeners** (single process) - ~434 LOC
- **Code Ratio**: Denali approach is ~5x less code to maintain
- Both differ in tracking:
  - **CSI Driver-Owned**: Explicit reference counting in metadata
  - **Denali-Owned**: Implicit sharing via NFS4 kernel multiplexing + mount table query

---

## Ownership Model

### CSI Driver-Owned
```
Ownership: CSI Driver Container
├─> Controls tunnel lifecycle
├─> Manages stunnel processes
├─> Maintains metadata
├─> Performs health checks
└─> Communicates via gRPC

Tunnel Manager Container:
├─> Runs as separate container
├─> Exposes gRPC service
├─> Manages stunnel processes
├─> Persists metadata to disk
└─> Independent lifecycle
```

### Denali-Owned
```
Ownership: denali-stunnel Container
├─> Owns stunnel process
├─> Polls for config changes
├─> Auto-reloads on changes
├─> Manages TLS connections
└─> Independent of CSI driver

CSI Driver Container:
├─> Writes config files
├─> Sends SIGHUP signals
├─> Queries mount table
├─> Manages port allocation
└─> No direct process control
```

---

## Use Case Recommendations

### Choose CSI Driver-Owned When:
- ✅ Need explicit reference counting
- ✅ Require active health monitoring
- ✅ Want persistent state across restarts
- ✅ Need detailed failure recovery
- ✅ Complex multi-tenant scenarios
- ✅ Require audit trail (metadata files)
- ✅ Need programmatic control via gRPC

### Choose Denali-Owned When:
- ✅ Prefer simpler architecture
- ✅ Want container-level isolation
- ✅ Rely on NFS4 multiplexing
- ✅ Need faster implementation
- ✅ Kubernetes-native recovery acceptable
- ✅ Lower operational complexity desired
- ✅ File-based configuration preferred

---

## Migration Considerations

### From CSI Driver-Owned to Denali-Owned
- **Pros**: Simpler architecture, lower complexity, container isolation
- **Cons**: Lose persistent metadata, lose active health checks, ephemeral state
- **Migration**: Requires pod restart, tunnels recreated from scratch
- **Risk**: Brief downtime during migration

### From Denali-Owned to CSI Driver-Owned
- **Pros**: Persistent state, active monitoring, explicit reference counting
- **Cons**: Higher complexity, additional gRPC layer, more resources
- **Migration**: Requires pod restart, metadata files created
- **Risk**: Brief downtime during migration

---

## Outstanding Questions

### Common to Both Approaches

1. **Certificate Expiry Management**
   - **Question**: How should certificate expiry be handled for stunnel containers (both tunnel-manager and denali-stunnel)?
   - **Options**:
     - **Option 1**: Release new image before expiry (requires expiry tracking, release process, and customer updates)
     - **Option 2**: Mount certificates via volume (allows updates without image rebuild or pod restart)
     - **Option 3**: Use cert-manager or similar for automatic rotation (Kubernetes-native approach)
   - **Impact**: Critical for production deployments - expired certificates will break all NFS mounts
   - **Decision needed**: Choose certificate lifecycle strategy for both approaches

### Denali-Owned Specific

2. **Recovery Mechanism Clarification**
   - **Clarified**: `recoverExistingTunnels()` is for CSI driver's port allocation map, NOT for stunnel
   - **Stunnel**: Automatically reads all `.conf` files on startup (no recovery needed)
   - **CSI Driver**: Needs to rebuild port map to prevent allocation conflicts after restart

---

## Summary

| Aspect | CSI Driver-Owned | Denali-Owned |
|--------|------------------|--------------|
| **Stunnel Architecture** | 1 process per file share | 1 process with multiple listeners |
| **Lines of Code** | ~2000 LOC | ~434 LOC |
| **Code Complexity** | Higher (gRPC, metadata, health) | Lower (file-based configs) |
| **State Persistence** | Persistent (metadata files) | Ephemeral (EmptyDir) |
| **Health Monitoring** | Active (continuous checks) | Passive (Kubernetes restarts) |
| **Recovery Time** | Fast (metadata-based) | Medium (config scan) |
| **Recovery Purpose** | Restore tunnel state + RefCounts | Rebuild port allocation map only |
| **Resource Usage** | Higher (multiple processes) | Lower (single process) |
| **Operational Overhead** | Higher (metadata management) | Lower (simple configs) |
| **Certificate Management** | Needs expiry strategy | Needs expiry strategy |
| **Maintainability** | More code to maintain | Less code to maintain (~5x reduction) |
| **Best For** | Enterprise, multi-tenant | Standard deployments |
| **Maturity** | Production-ready (cert strategy needed) | Production-ready (cert strategy needed) |

**Key Architectural Difference:**
- **CSI Driver-Owned**: Multiple stunnel processes (1 per file share) - ~2000 LOC
- **Denali-Owned**: Single stunnel process with multiple listeners (1 listener per file share) - ~434 LOC
- **Code Maintenance**: Denali approach requires ~5x less code to maintain

Both approaches are **production-ready**. The choice depends on operational requirements, complexity tolerance, and desired recovery characteristics. **Both require a certificate expiry management strategy.**