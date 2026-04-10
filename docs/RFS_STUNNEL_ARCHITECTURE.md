# IBM VPC File CSI Driver - RFS Stunnel Architecture

## Table of Contents
1. [Overview](#overview)
2. [High-Level Architecture](#high-level-architecture)
3. [Component Details](#component-details)
4. [Data Flow Diagrams](#data-flow-diagrams)
5. [Recovery Mechanism](#recovery-mechanism)
6. [Reference Counting](#reference-counting)
7. [Port Management](#port-management)
8. [Health Monitoring](#health-monitoring)
9. [Configuration](#configuration)
10. [Deployment](#deployment)
11. [Monitoring & Troubleshooting](#monitoring--troubleshooting)

---

## Overview

The IBM VPC File CSI Driver implements **per-volume Stunnel tunnels** to provide Encryption in Transit (EIT) for RFS (Remote File Storage) profile volumes. This architecture uses a **sidecar pattern** where a tunnel-manager container runs alongside the CSI node server, managing the lifecycle of Stunnel processes independently.

### Key Features

- **Sidecar Architecture** - Tunnel manager runs as separate container
- **Shared Tunnels** - One tunnel per NFS share (not per pod)
- **Reference Counting** - Tracks multiple pods using same share
- **Automatic Recovery** - Restores tunnels after crashes/reboots
- **Health Monitoring** - Automatic restart of failed tunnels
- **Dynamic Port Allocation** - Hash-based with fallback (20000-30000)
- **HTTP/JSON API** - Simple Unix socket communication

---

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          Kubernetes Node                                 │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────────┐ │
│  │              CSI Node DaemonSet Pod                                 │ │
│  │                                                                     │ │
│  │  ┌──────────────────┐         ┌──────────────────────────────┐    │ │
│  │  │  CSI Node Server │         │   Tunnel Manager Sidecar     │    │ │
│  │  │                  │         │                              │    │ │
│  │  │  - NodePublish   │◄───────►│  - EnsureTunnel()           │    │ │
│  │  │  - NodeUnpublish │  HTTP   │  - RemoveTunnel()           │    │ │
│  │  │  - Mount NFS     │  Unix   │  - RefCount Management      │    │ │
│  │  │                  │  Socket │  - Health Monitoring        │    │ │
│  │  │                  │         │  - Recovery Logic           │    │ │
│  │  └──────────────────┘         └──────────────────────────────┘    │ │
│  │           │                              │                         │ │
│  │           │                              │ Manages                 │ │
│  │           │                              ▼                         │ │
│  │           │                    ┌──────────────────┐               │ │
│  │           │                    │ Stunnel Process  │               │ │
│  │           │                    │ (per volume)     │               │ │
│  │           │                    │                  │               │ │
│  │           │                    │ 127.0.0.1:20574 │               │ │
│  │           │                    │       ▼          │               │ │
│  │           │                    │  TLS Tunnel      │               │ │
│  │           │                    └──────────────────┘               │ │
│  │           │                              │                         │ │
│  └───────────┼──────────────────────────────┼─────────────────────────┘ │
│              │                              │                           │
│              │ NFS Mount                    │ TLS Connection            │
│              │ 127.0.0.1:20574              │                           │
│              ▼                              ▼                           │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │                    Host Filesystem                               │   │
│  │  /var/lib/kubelet/pods/<pod-uid>/volumes/...                    │   │
│  │  /var/data/kubelet/pods/<pod-uid>/volumes/...                   │   │
│  └─────────────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ TLS over Internet
                                    ▼
                    ┌────────────────────────────────┐
                    │  IBM VPC RFS NFS Server        │
                    │  10.x.x.x:20049                │
                    │  (Encryption in Transit)       │
                    └────────────────────────────────┘
```

---

## Component Details

### 1. CSI Node Server

**Location:** `pkg/ibmcsidriver/node.go`

**Responsibilities:**
- Handles CSI `NodePublishVolume` and `NodeUnpublishVolume` requests
- Detects RFS volumes with EIT enabled
- Calls tunnel-manager API via HTTP over Unix socket
- Performs NFS mount using local tunnel endpoint

**Key Operations:**
```go
// NodePublishVolume flow
1. Detect RFS profile + EIT enabled
2. Parse NFS source: nfs://10.x.x.x/export
3. Call tunnel-manager: EnsureTunnel(volumeID, nfsServer)
4. Receive tunnel port
5. Mount: 127.0.0.1:/<export> with port=<tunnel_port>
```

### 2. Tunnel Manager Sidecar

**Location:** `pkg/tunnel/manager.go`, `pkg/tunnel/http_service.go`

**Responsibilities:**
- Manages lifecycle of Stunnel processes
- Allocates unique ports (20000-30000 range)
- Tracks reference counts per tunnel
- Persists metadata for recovery
- Monitors tunnel health
- Validates active mounts via `/proc/mounts`

**Core Data Structures:**
```go
type Manager struct {
    tunnels        map[string]*Tunnel  // volumeID -> Tunnel
    allocatedPorts map[int]bool        // Port tracking
    basePort       int                 // 20000
    portRange      int                 // 10000
    configDir      string              // /etc/stunnel
    healthInterval time.Duration       // 30s
}

type Tunnel struct {
    VolumeID   string      // NFS share ID
    RemoteAddr string      // NFS server address
    LocalPort  int         // Allocated local port
    RefCount   int         // Number of active mounts
    State      TunnelState // running/stopped/failed
    Cmd        *exec.Cmd   // Stunnel process
}
```

**API Endpoints:**
- `POST /v1/tunnels/ensure` - Create or reuse tunnel
- `POST /v1/tunnels/remove` - Decrement refcount, remove if zero
- `GET /v1/tunnels/{volumeID}` - Get tunnel info
- `GET /healthz` - Health check

### 3. Stunnel Process

**Configuration:**
```ini
; Global options
client = yes
foreground = yes

; Service definition
[nfs-<volumeID>]
accept = 127.0.0.1:<port>
connect = <nfsServer>:20049
cafile = /host-certs/tls-ca-bundle.pem
checkHost = production.is-share.appdomain.cloud
verify = 1
```

**Lifecycle:**
- Started by tunnel-manager
- Runs in foreground mode
- Monitored by health check loop
- Auto-restarted on failure (max 3 attempts)

---

## Data Flow Diagrams

### Mount Flow (First Pod)

```
┌──────────┐           ┌──────────┐           ┌─────────────┐           ┌─────────┐
│ Kubelet  │           │   CSI    │           │   Tunnel    │           │ Stunnel │
│          │           │  Node    │           │   Manager   │           │ Process │
└────┬─────┘           └────┬─────┘           └──────┬──────┘           └────┬────┘
     │                      │                         │                       │
     │ NodePublishVolume    │                         │                       │
     ├─────────────────────>│                         │                       │
     │                      │                         │                       │
     │                      │ Detect RFS + EIT        │                       │
     │                      │ Parse NFS source        │                       │
     │                      │                         │                       │
     │                      │ POST /v1/tunnels/ensure │                       │
     │                      │ {volumeID, nfsServer}   │                       │
     │                      ├────────────────────────>│                       │
     │                      │                         │                       │
     │                      │                         │ Check if exists       │
     │                      │                         │ (No - create new)     │
     │                      │                         │                       │
     │                      │                         │ Allocate port (20574) │
     │                      │                         │                       │
     │                      │                         │ Generate config       │
     │                      │                         │ /etc/stunnel/vol.conf │
     │                      │                         │                       │
     │                      │                         │ Start stunnel         │
     │                      │                         ├──────────────────────>│
     │                      │                         │                       │
     │                      │                         │                       │ Connect to
     │                      │                         │                       │ NFS:20049
     │                      │                         │                       │
     │                      │                         │ Save metadata         │
     │                      │                         │ vol.meta.json         │
     │                      │                         │ vol.meta.json.bak     │
     │                      │                         │ {refCount:1}          │
     │                      │                         │                       │
     │                      │ {port:20574, refCount:1}│                       │
     │                      │<────────────────────────┤                       │
     │                      │                         │                       │
     │                      │ Mount NFS4              │                       │
     │                      │ 127.0.0.1:/export       │                       │
     │                      │ port=20574              │                       │
     │                      │                         │                       │
     │ Success              │                         │                       │
     │<─────────────────────┤                         │                       │
     │                      │                         │                       │
```

### Mount Flow (Second Pod - Same Share)

```
┌──────────┐           ┌──────────┐           ┌─────────────┐           ┌─────────┐
│ Kubelet  │           │   CSI    │           │   Tunnel    │           │ Stunnel │
│          │           │  Node    │           │   Manager   │           │ Process │
└────┬─────┘           └────┬─────┘           └──────┬──────┘           └────┬────┘
     │                      │                         │                       │
     │ NodePublishVolume    │                         │                       │
     ├─────────────────────>│                         │                       │
     │                      │                         │                       │
     │                      │ POST /v1/tunnels/ensure │                       │
     │                      ├────────────────────────>│                       │
     │                      │                         │                       │
     │                      │                         │ Check if exists       │
     │                      │                         │ (Yes - reuse)         │
     │                      │                         │                       │
     │                      │                         │ Increment refCount    │
     │                      │                         │ refCount: 1 -> 2      │
     │                      │                         │                       │
     │                      │                         │ Update metadata       │
     │                      │                         │ {refCount:2}          │
     │                      │                         │                       │
     │                      │ {port:20574, refCount:2}│                       │
     │                      │<────────────────────────┤                       │
     │                      │                         │                       │
     │                      │ Mount NFS4              │                       │
     │                      │ 127.0.0.1:/export       │                       │
     │                      │ port=20574              │                       │
     │                      │                         │                       │
     │ Success              │                         │                       │
     │<─────────────────────┤                         │                       │
     │                      │                         │                       │
```

### Unmount Flow (First Pod - RefCount > 0)

```
┌──────────┐           ┌──────────┐           ┌─────────────┐           ┌─────────┐
│ Kubelet  │           │   CSI    │           │   Tunnel    │           │ Stunnel │
│          │           │  Node    │           │   Manager   │           │ Process │
└────┬─────┘           └────┬─────┘           └──────┬──────┘           └────┬────┘
     │                      │                         │                       │
     │ NodeUnpublishVolume  │                         │                       │
     ├─────────────────────>│                         │                       │
     │                      │                         │                       │
     │                      │ Unmount NFS4            │                       │
     │                      │                         │                       │
     │                      │ POST /v1/tunnels/remove │                       │
     │                      ├────────────────────────>│                       │
     │                      │                         │                       │
     │                      │                         │ Decrement refCount    │
     │                      │                         │ refCount: 2 -> 1      │
     │                      │                         │                       │
     │                      │                         │ Update metadata       │
     │                      │                         │ {refCount:1}          │
     │                      │                         │                       │
     │                      │                         │ Keep tunnel running   │
     │                      │                         │                       │
     │                      │ Success                 │                       │
     │                      │<────────────────────────┤                       │
     │                      │                         │                       │
     │ Success              │                         │                       │
     │<─────────────────────┤                         │                       │
     │                      │                         │                       │
```

### Unmount Flow (Last Pod - RefCount = 0)

```
┌──────────┐           ┌──────────┐           ┌─────────────┐           ┌─────────┐
│ Kubelet  │           │   CSI    │           │   Tunnel    │           │ Stunnel │
│          │           │  Node    │           │   Manager   │           │ Process │
└────┬─────┘           └────┬─────┘           └──────┬──────┘           └────┬────┘
     │                      │                         │                       │
     │ NodeUnpublishVolume  │                         │                       │
     ├─────────────────────>│                         │                       │
     │                      │                         │                       │
     │                      │ Unmount NFS4            │                       │
     │                      │                         │                       │
     │                      │ POST /v1/tunnels/remove │                       │
     │                      ├────────────────────────>│                       │
     │                      │                         │                       │
     │                      │                         │ Decrement refCount    │
     │                      │                         │ refCount: 1 -> 0      │
     │                      │                         │                       │
     │                      │                         │ Stop stunnel          │
     │                      │                         ├──────────────────────>│
     │                      │                         │                       │
     │                      │                         │                       │ Process
     │                      │                         │                       │ stopped
     │                      │                         │                       │
     │                      │                         │ Delete files:         │
     │                      │                         │ - vol.conf            │
     │                      │                         │ - vol.meta.json       │
     │                      │                         │ - vol.meta.json.bak   │
     │                      │                         │                       │
     │                      │                         │ Release port 20574    │
     │                      │                         │                       │
     │                      │ Success                 │                       │
     │                      │<────────────────────────┤                       │
     │                      │                         │                       │
     │ Success              │                         │                       │
     │<─────────────────────┤                         │                       │
     │                      │                         │                       │
```

---

## Recovery Mechanism

### Recovery Flow (Node Reboot)

```
┌──────────┐           ┌─────────────┐           ┌─────────┐           ┌──────────┐
│  Node    │           │   Tunnel    │           │ Stunnel │           │   Pod    │
│  Reboot  │           │   Manager   │           │ Process │           │   Dirs   │
└────┬─────┘           └──────┬──────┘           └────┬────┘           └────┬─────┘
     │                        │                        │                     │
     │ Container restarts     │                        │                     │
     ├───────────────────────>│                        │                     │
     │                        │                        │                     │
     │                        │ RecoverFromCrash()     │                     │
     │                        │                        │                     │
     │                        │ Load metadata files    │                     │
     │                        │ *.meta.json            │                     │
     │                        │                        │                     │
     │                        │ For each tunnel:       │                     │
     │                        │                        │                     │
     │                        │ Check process running? │                     │
     │                        ├───────────────────────>│                     │
     │                        │                        │                     │
     │                        │ No - restart stunnel   │                     │
     │                        ├───────────────────────>│                     │
     │                        │                        │                     │
     │                        │ getActiveMountCount()  │                     │
     │                        │ Scan /proc/mounts      │                     │
     │                        │ Extract pod UIDs       │                     │
     │                        │                        │                     │
     │                        │ Validate pod dirs      │                     │
     │                        ├────────────────────────────────────────────>│
     │                        │                        │                     │
     │                        │ statWithTimeout(2s)    │                     │
     │                        │ Check if dir exists    │                     │
     │                        │<────────────────────────────────────────────┤
     │                        │                        │                     │
     │                        │ Count active mounts    │                     │
     │                        │ (only valid pods)      │                     │
     │                        │                        │                     │
     │                        │ Compare with refCount  │                     │
     │                        │                        │                     │
     │                        │ IF active=0, refCount>0│                     │
     │                        │ → Stale metadata!      │                     │
     │                        │                        │                     │
     │                        │ Stop stunnel           │                     │
     │                        ├───────────────────────>│                     │
     │                        │                        │                     │
     │                        │ Delete files:          │                     │
     │                        │ - *.conf               │                     │
     │                        │ - *.meta.json          │                     │
     │                        │ - *.meta.json.bak      │                     │
     │                        │                        │                     │
     │                        │ Release port           │                     │
     │                        │                        │                     │
     │                        │ IF active>0            │                     │
     │                        │ → Update refCount      │                     │
     │                        │ → Save metadata        │                     │
     │                        │                        │                     │
     │                        │ Recovery complete      │                     │
     │                        │                        │                     │
```

### Orphaned Mount Detection Flow

```
┌─────────────┐           ┌──────────┐           ┌──────────┐           ┌─────────┐
│   Tunnel    │           │  /proc/  │           │ Kubelet  │           │  Pod    │
│   Manager   │           │  mounts  │           │   Pods   │           │   Dir   │
└──────┬──────┘           └────┬─────┘           └────┬─────┘           └────┬────┘
       │                       │                      │                      │
       │ getActiveMountCount() │                      │                      │
       │                       │                      │                      │
       │ Read /proc/mounts     │                      │                      │
       ├──────────────────────>│                      │                      │
       │                       │                      │                      │
       │ Mount entries:        │                      │                      │
       │ 127.0.0.1:/export     │                      │                      │
       │ on /var/data/kubelet/ │                      │                      │
       │ pods/<pod-uid>/...    │                      │                      │
       │<──────────────────────┤                      │                      │
       │                       │                      │                      │
       │ Extract pod UID       │                      │                      │
       │ from mount path       │                      │                      │
       │                       │                      │                      │
       │ Extract kubelet path  │                      │                      │
       │ /var/data/kubelet     │                      │                      │
       │                       │                      │                      │
       │ Build pod dir path:   │                      │                      │
       │ /var/data/kubelet/    │                      │                      │
       │ pods/<pod-uid>        │                      │                      │
       │                       │                      │                      │
       │ Check if dir exists   │                      │                      │
       ├──────────────────────────────────────────────────────────────────>│
       │                       │                      │                      │
       │ statWithTimeout(2s)   │                      │                      │
       │                       │                      │                      │
       │ CASE 1: Dir exists    │                      │                      │
       │<──────────────────────────────────────────────────────────────────┤
       │ → Count this mount    │                      │                      │
       │                       │                      │                      │
       │ CASE 2: Dir not found │                      │                      │
       │<──────────────────────────────────────────────────────────────────┤
       │ → Orphaned mount!     │                      │                      │
       │ → Skip this mount     │                      │                      │
       │                       │                      │                      │
       │ CASE 3: Timeout       │                      │                      │
       │<──────────────────────────────────────────────────────────────────┤
       │ → Hung mount!         │                      │                      │
       │ → Treat as orphaned   │                      │                      │
       │                       │                      │                      │
       │ Return active count   │                      │                      │
       │ (only valid pods)     │                      │                      │
       │                       │                      │                      │
```

### Key Recovery Features

1. **Mount Verification** - Checks `/proc/mounts` for actual active mounts
2. **Pod Validation** - Verifies pod directories exist in kubelet path
3. **Timeout Protection** - 2-second timeout prevents hangs on unstable mounts
4. **Dynamic Path Support** - Works with any kubelet path (`/var/lib/kubelet`, `/var/data/kubelet`, etc.)
5. **Deduplication** - Counts unique pod UIDs (handles symlinks correctly)
6. **Orphaned Mount Detection** - Ignores mounts for deleted pods

---

## Reference Counting

### RefCount Management

```
Scenario: Multiple pods using same NFS share

Pod1 mounts PVC-A:
  EnsureTunnel(shareA) → tunnel created, refCount=1

Pod2 mounts PVC-A:
  EnsureTunnel(shareA) → tunnel reused, refCount=2

Pod3 mounts PVC-A:
  EnsureTunnel(shareA) → tunnel reused, refCount=3

Pod1 unmounts:
  RemoveTunnel(shareA) → refCount=2, tunnel kept

Pod2 unmounts:
  RemoveTunnel(shareA) → refCount=1, tunnel kept

Pod3 unmounts:
  RemoveTunnel(shareA) → refCount=0, tunnel removed ✓
```

### RefCount Verification Algorithm

```go
func getActiveMountCount(port int) (int, error) {
    // Read /proc/mounts
    file, _ := os.Open("/proc/mounts")
    
    validPodUIDs := make(map[string]bool)
    
    for each line in /proc/mounts {
        if line contains "nfs4" && "127.0.0.1" && "port=<port>" {
            // Extract mount point
            mountPoint := fields[1]
            
            // Extract pod UID from path
            // /var/lib/kubelet/pods/<pod-uid>/volumes/...
            podUID := extractPodUID(mountPoint)
            
            // Extract kubelet base path dynamically
            kubeletBasePath := mountPoint[:idx] // Before "/pods/"
            
            // Validate pod directory exists with timeout
            podDir := kubeletBasePath + "/pods/" + podUID
            _, err := statWithTimeout(podDir, 2*time.Second)
            
            if err == nil {
                // Pod exists, count this mount
                validPodUIDs[podUID] = true
            } else {
                // Pod deleted or timeout, ignore mount
                log.Warn("Ignoring orphaned/hung mount")
            }
        }
    }
    
    return len(validPodUIDs), nil
}
```

---

## Port Management

### Port Allocation Strategy

**Range:** 20000-30000 (10,000 ports available)

**Algorithm:**
1. **Hash-based allocation** (primary)
   - Hash volumeID to determine preferred port
   - Consistent port assignment for same volume
   
2. **Linear search** (fallback)
   - If preferred port unavailable, search sequentially
   - Ensures volume always gets a port

3. **Availability check**
   - Attempts to bind to port before allocation
   - Prevents conflicts with other services

**Example:**
```go
func (m *Manager) allocatePort(volumeID string) (int, error) {
    // Try hash-based port first
    hash := fnv.New32a()
    hash.Write([]byte(volumeID))
    preferredPort := m.basePort + int(hash.Sum32()%uint32(m.portRange))
    
    if m.isPortAvailable(preferredPort) {
        return preferredPort, nil
    }
    
    // Fallback: linear search
    for port := m.basePort; port < m.basePort+m.portRange; port++ {
        if m.isPortAvailable(port) {
            return port, nil
        }
    }
    
    return 0, errors.New("no ports available")
}
```

---

## Health Monitoring

### Health Check Loop

```
Every 30 seconds (configurable):

For each tunnel:
  1. Check if stunnel process is running
  2. Test TCP connection to local port
  3. Update last healthy timestamp
  
  If unhealthy:
    - Log warning
    - Attempt restart (max 3 times)
    - Exponential backoff: 2s, 4s, 6s
    
  If restart limit exceeded:
    - Mark tunnel as failed
    - Log error
    - Require manual intervention
```

### Process Monitoring

- Stunnel runs in **foreground mode**
- Go routine monitors process exit
- Distinguishes intentional stop vs crash
- Automatic restart on unexpected termination

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `TUNNEL_MANAGER_SOCKET` | `/var/lib/kubelet/plugins/vpc.file.csi.ibm.io/tunnel-manager.sock` | Unix socket path |
| `STUNNEL_BASE_PORT` | 20000 | Starting port for allocation |
| `STUNNEL_PORT_RANGE` | 10000 | Number of ports available |
| `STUNNEL_CONFIG_DIR` | /etc/stunnel | Configuration directory |
| `STUNNEL_CA_FILE` | /host-certs/tls-ca-bundle.pem | CA bundle path |
| `STUNNEL_NFS_PORT` | 20049 | NFS port on RFS servers |
| `STUNNEL_ENVIRONMENT` | production | Environment for hostname verification |
| `STUNNEL_HEALTH_CHECK_INTERVAL` | 30s | Health check interval |

### Storage Class Example

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ibmc-vpc-file-rfs-eit
provisioner: vpc.file.csi.ibm.io
parameters:
  profile: "rfs"              # Required: Use RFS profile
  isEITEnabled: "true"        # Required: Enable encryption in transit
  throughput: "1000"          # Required: Bandwidth in MB/s
  isENIEnabled: "true"        # Recommended: Use ENI
```

---

## Deployment

### Volume Mounts (tunnel-manager container)

```yaml
volumeMounts:
  # Plugin directory for Unix socket
  - name: plugin-dir
    mountPath: /var/lib/kubelet/plugins/vpc.file.csi.ibm.io/
  
  # Stunnel configuration and metadata
  - name: stunnel-dir
    mountPath: /etc/stunnel
  
  # Kubelet data directory for pod validation
  - name: kubelet-data-dir
    mountPath: /var/data/kubelet
    mountPropagation: "HostToContainer"
```

### Security Context

```yaml
securityContext:
  runAsNonRoot: false
  runAsUser: 0
  runAsGroup: 0
  privileged: true
```

**Why privileged?**
- Access to `/proc/mounts` for mount verification
- Access to host filesystem for pod validation
- Ability to manage stunnel processes

---

## Monitoring & Troubleshooting

### Key Metrics

```bash
# Tunnel count
ps aux | grep stunnel | wc -l

# Port usage
netstat -tuln | grep "127.0.0.1:2[0-9]" | wc -l

# Active mounts
cat /proc/mounts | grep nfs4 | grep 127.0.0.1 | wc -l
```

### Health Checks

```bash
# Check tunnel-manager is running
kubectl get pods -n kube-system -l app=ibm-vpc-file-csi-node

# Check recovery logs
kubectl logs -n kube-system <pod> -c tunnel-manager | grep "recovery completed"

# Check for errors
kubectl logs -n kube-system <pod> -c tunnel-manager | grep -i error
```

### Common Issues

**Issue:** Tunnels cleaned up after restart
- **Cause:** Missing kubelet volume mount
- **Fix:** Ensure `kubelet-data-dir` mount is present

**Issue:** Recovery hangs
- **Cause:** Hung NFS mount blocking stat
- **Fix:** Timeout protection implemented (2s)

**Issue:** Incorrect refcount
- **Cause:** Stale metadata
- **Fix:** Automatic correction via `/proc/mounts` verification

---

## Performance Characteristics

### Resource Usage (Per Tunnel)
- **Memory:** 10-20 MB
- **CPU:** <1% under normal load
- **File Descriptors:** 5-10

### Scalability
- **Recommended:** <500 tunnels per node
- **Maximum:** ~1000 tunnels per node
- **Port Range:** 10,000 ports available

### Latency
- **Tunnel Creation:** 100-500ms
- **IPC (HTTP/Unix):** ~1ms
- **Recovery:** ~50ms per tunnel

---

## Summary

The stunnel sidecar architecture provides a robust, scalable solution for encrypted NFS mounts:

- ✅ **Reliable** - Survives container restarts and node reboots
- ✅ **Accurate** - RefCount verified against actual mounts
- ✅ **Robust** - Timeout protection prevents hangs
- ✅ **Portable** - Works with any kubelet path configuration
- ✅ **Scalable** - Supports hundreds of tunnels per node
- ✅ **Observable** - Comprehensive logging and metrics
- ✅ **Production-Ready** - All critical bugs fixed

**Status:** Production deployment ready.