# IBM VPC File CSI Driver - Stunnel Architecture

## Table of Contents
- [Overview](#overview)
- [System Architecture](#system-architecture)
- [Component Design](#component-design)
- [Data Flow Diagrams](#data-flow-diagrams)
- [Security Model](#security-model)
- [High Availability](#high-availability)

---

## Overview

The IBM VPC File CSI Driver uses **stunnel** to provide secure TLS tunnels for NFS connections to IBM Cloud VPC File Storage (RFS - Remote File Storage). This architecture enables secure, encrypted communication between Kubernetes pods and NFS servers.

### Key Features
- ✅ **TLS Encryption**: All NFS traffic encrypted via stunnel
- ✅ **Dynamic Port Management**: Automatic port allocation (20000-30000)
- ✅ **Reference Counting**: Tracks active mounts per tunnel
- ✅ **Automatic Recovery**: Restarts failed tunnels, cleans up stale metadata
- ✅ **gRPC Communication**: Efficient IPC between CSI driver and tunnel manager
- ✅ **OS-Agnostic**: Works on RHEL/RHCOS and Ubuntu nodes

---

## System Architecture

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Kubernetes Node                              │
│                                                                       │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                    CSI Node DaemonSet                         │  │
│  │                                                                │  │
│  │  ┌─────────────────────┐      ┌──────────────────────────┐  │  │
│  │  │  CSI Node Driver    │      │   Tunnel Manager         │  │  │
│  │  │  (iks-vpc-file-     │◄────►│   (Sidecar Container)    │  │  │
│  │  │   node-driver)      │ gRPC │                          │  │  │
│  │  │                     │ Unix │   - Port Management      │  │  │
│  │  │  - NodePublish      │Socket│   - Stunnel Lifecycle   │  │  │
│  │  │  - NodeUnpublish    │      │   - Health Monitoring    │  │  │
│  │  │  - Volume Mounting  │      │   - Metadata Tracking    │  │  │
│  │  └─────────────────────┘      └──────────────────────────┘  │  │
│  │           │                              │                    │  │
│  └───────────┼──────────────────────────────┼────────────────────┘  │
│              │                              │                        │
│              │ NFS Mount                    │ Manages                │
│              ↓                              ↓                        │
│  ┌─────────────────────┐      ┌──────────────────────────┐         │
│  │  /var/lib/kubelet/  │      │   Stunnel Processes      │         │
│  │  pods/.../volumes/  │      │   (One per volume)       │         │
│  │                     │      │                          │         │
│  │  127.0.0.1:20XXX    │◄─────┤   stunnel (PID 1234)    │         │
│  │  (Local NFS mount)  │      │   Port: 20574           │         │
│  └─────────────────────┘      │   Config: vol-abc.conf  │         │
│                                └──────────────────────────┘         │
│                                           │                          │
│                                           │ TLS Tunnel               │
└───────────────────────────────────────────┼──────────────────────────┘
                                            │
                                            ↓
                              ┌──────────────────────────┐
                              │  IBM Cloud VPC Network   │
                              │                          │
                              │  ┌────────────────────┐ │
                              │  │  RFS NFS Server    │ │
                              │  │  (File Share)      │ │
                              │  │  10.240.x.x:20049  │ │
                              │  └────────────────────┘ │
                              └──────────────────────────┘
```

### Container Architecture with Volume Mounts

```
┌────────────────────────────────────────────────────────────────────────┐
│                         Node Server Pod                                 │
│                                                                          │
│  ┌────────────────────────────┐  ┌──────────────────────────────┐     │
│  │  iks-vpc-file-node-driver  │  │  iks-vpc-file-tunnel-mgr     │     │
│  │                            │  │                              │     │
│  │  Volume Mounts:            │  │  Volume Mounts:              │     │
│  │  - /var/lib/kubelet        │  │  - /etc/stunnel              │     │
│  │    (host kubelet dir)      │  │    (configs & metadata)      │     │
│  │  - /dev                    │  │  - /host-certs               │     │
│  │    (host devices)          │  │    (host /etc for CA certs)  │     │
│  │  - /csi                    │  │  - /csi                      │     │
│  │    (shared plugin dir)     │  │    (shared plugin dir)       │     │
│  │                            │  │  - /proc/mounts              │     │
│  │  Environment:              │  │    (host mount table)        │     │
│  │  - NODE_OS_TYPE            │  │                              │     │
│  │    (from node label)       │  │  Environment:                │     │
│  │                            │  │  - NODE_OS_TYPE              │     │
│  │                            │  │  - STUNNEL_ENVIRONMENT       │     │
│  └────────────────────────────┘  └──────────────────────────────┘     │
│              │                                  │                       │
│              └──────────────┬───────────────────┘                       │
│                             │                                           │
│                    Shared Volume: plugin-dir                            │
│                    Container Path: /csi                                 │
│                    Host Path: /var/lib/kubelet/plugins/vpc.file.csi... │
│                                                                          │
│                    Unix Socket (gRPC):                                  │
│                    /var/lib/kubelet/plugins/vpc.file.csi.ibm.io/        │
│                    tunnel-manager.sock                                  │
│                                                                          │
│                    (Accessed as /csi/tunnel-manager.sock in containers) │
└────────────────────────────────────────────────────────────────────────┘
```

### Detailed Volume Mapping

```
Host System                          Container (Both CSI Driver & Tunnel Manager)
─────────────────────────────────    ────────────────────────────────────────────

/var/lib/kubelet/                    
├── plugins/                         
│   └── vpc.file.csi.ibm.io/    ──► /csi/
│       ├── tunnel-manager.sock      ├── tunnel-manager.sock (gRPC socket)
│       └── csi.sock                 └── csi.sock (CSI socket)
│
├── pods/                        ──► /var/lib/kubelet/pods/
│   └── <pod-uid>/                   └── <pod-uid>/
│       └── volumes/                     └── volumes/
│           └── kubernetes.io~csi/           └── kubernetes.io~csi/
│
/etc/                            ──► /host-certs/
├── pki/                             ├── pki/
│   └── ca-trust/                    │   └── ca-trust/
│       └── extracted/               │       └── extracted/
│           └── pem/                 │           └── pem/
│               └── tls-ca-bundle.pem│               └── tls-ca-bundle.pem
└── ssl/                             └── ssl/
    └── certs/                           └── certs/
        └── ca-certificates.crt              └── ca-certificates.crt

/etc/stunnel/                    ──► /etc/stunnel/ (Tunnel Manager only)
├── vol-abc123.conf                  ├── vol-abc123.conf
├── vol-abc123.pid                   ├── vol-abc123.pid
├── vol-abc123.meta                  ├── vol-abc123.meta
└── logs/                            └── logs/

/proc/mounts                     ──► /proc/mounts (Tunnel Manager only, read-only)

/dev/                            ──► /dev/ (CSI Driver only)
```

---

## Component Design

### 1. Tunnel Manager

**Responsibilities:**
- Manage stunnel process lifecycle (start, stop, monitor)
- Allocate and track local ports (20000-30000 range)
- Maintain reference counts for shared tunnels
- Recover tunnels after pod restarts
- Health monitoring and automatic restart

**Key Components:**

```go
type Manager struct {
    tunnels      map[string]*Tunnel  // volumeID -> Tunnel
    portAllocator *PortAllocator
    logger       *zap.Logger
    mu           sync.RWMutex
}

type Tunnel struct {
    VolumeID     string
    RemoteAddr   string      // RFS server IP:port
    LocalPort    int         // Allocated local port
    Cmd          *exec.Cmd   // Stunnel process
    RefCount     int         // Number of active mounts
    State        TunnelState
    ConfigPath   string
    PIDFile      string
}
```

**Port Allocation Strategy:**
```
Base Port: 20000
Range: 10000 ports (20000-29999)

Algorithm:
1. Hash volumeID to get preferred port
2. If port in use, linear probe for next available
3. Track allocated ports in bitmap
4. Release ports when RefCount reaches 0
```

### 2. gRPC Service

**Proto Definition:**
```protobuf
service TunnelManager {
    rpc EnsureTunnel(EnsureTunnelRequest) returns (EnsureTunnelResponse);
    rpc RemoveTunnel(RemoveTunnelRequest) returns (RemoveTunnelResponse);
    rpc GetTunnel(GetTunnelRequest) returns (GetTunnelResponse);
    rpc ListTunnels(ListTunnelsRequest) returns (ListTunnelsResponse);
    rpc Health(HealthRequest) returns (HealthResponse);
}
```

**Socket Path:**
- **Code constant**: `/var/lib/kubelet/plugins/vpc.file.csi.ibm.io/tunnel-manager.sock`
- **Container access**: `/csi/tunnel-manager.sock` (via volume mount)
- **Shared between**: CSI driver and tunnel manager containers

### 3. Stunnel Configuration

**Generated Config Template:**
```ini
; Stunnel configuration for volume vol-abc123
; Generated at 2026-04-13T10:30:00Z
; This configuration runs with host network access for NFS4 mounting

; Global options
client = yes
foreground = yes

; Service definition for NFS over TLS
[nfs-vol-abc123]
accept = 127.0.0.1:20574
connect = 10.240.128.5:20049
cafile = /host-certs/pki/ca-trust/extracted/pem/tls-ca-bundle.pem
checkHost = production.is-share.appdomain.cloud
verify = 1
```

**Configuration Parameters:**
- `client = yes`: Stunnel operates in client mode (connects to remote server)
- `foreground = yes`: Run in foreground (required for container lifecycle)
- `accept`: Local port where NFS client connects (127.0.0.1:20574)
- `connect`: Remote RFS server address (10.240.128.5:20049)
- `cafile`: Path to CA bundle for TLS verification
- `checkHost`: Expected hostname in server certificate (environment-specific)
- `verify = 1`: Verify peer certificate

**CA Bundle Path Detection:**
- **RHEL/RHCOS**: `/host-certs/pki/ca-trust/extracted/pem/tls-ca-bundle.pem`
- **Ubuntu**: `/host-certs/ssl/certs/ca-certificates.crt`
- Detection via `NODE_OS_TYPE` env var (from node label `ibm-cloud.kubernetes.io/os`)
- Fallback: filesystem detection (`/host-certs/redhat-release`)

**Environment-Specific checkHost:**
- **Production**: `production.is-share.appdomain.cloud`
- **Staging**: `staging.is-share.appdomain.cloud`
- Auto-detected from cluster master URL or set via `STUNNEL_ENVIRONMENT` env var

### 4. Metadata Persistence

**Metadata File Format:**

Each tunnel has two metadata files stored in `/etc/stunnel/`:
- Primary: `<volumeID>.meta.json`
- Backup: `<volumeID>.meta.json.bak`

**Sample Metadata File (`pvc-abc123.meta.json`):**
```json
{
  "volumeID": "pvc-abc123",
  "nfsServer": "10.240.128.5",
  "port": 20574,
  "refCount": 2
}
```

**Metadata Fields:**
- `volumeID`: Unique identifier for the volume (matches PVC name)
- `nfsServer`: IP address of the RFS NFS server
- `port`: Allocated local port for this tunnel (20000-29999 range)
- `refCount`: Number of active pod mounts using this tunnel

**Metadata Operations:**
- **Save**: Atomic write with retry (3 attempts, 200ms delay)
- **Load**: Primary file with automatic backup fallback
- **Validation**: Checks volumeID, nfsServer, port range, and refCount
- **Recovery**: Reads metadata on startup, verifies against `/proc/mounts`
- **Cleanup**: Deletes both primary and backup when tunnel is removed

**Metadata Validation Rules:**
- `volumeID` must not be empty
- `nfsServer` must not be empty
- `port` must be within configured range (default: 20000-29999)
- `refCount` must be >= 0

**Recovery Process:**
1. Scan `/etc/stunnel/*.meta.json` files on startup
2. Load metadata for each tunnel
3. Verify actual mount count from `/proc/mounts`
4. Correct refCount if mismatch detected
5. Clean up stale metadata (refCount = 0, no active mounts)
6. Restart tunnels with verified refCounts

---

## Data Flow Diagrams

### 1. Volume Mount Flow - New Tunnel Creation

```
┌──────────┐           ┌──────────┐           ┌─────────────┐           ┌─────────┐           ┌─────────┐
│ Kubelet  │           │   CSI    │           │   Tunnel    │           │ Stunnel │           │   RFS   │
│          │           │  Node    │           │   Manager   │           │ Process │           │  Server │
└────┬─────┘           └────┬─────┘           └──────┬──────┘           └────┬────┘           └────┬────┘
     │                      │                         │                       │                     │
     │ NodePublishVolume    │                         │                       │                     │
     │ (vol-abc123)         │                         │                       │                     │
     ├─────────────────────>│                         │                       │                     │
     │                      │                         │                       │                     │
     │                      │ Parse volume context    │                       │                     │
     │                      │ nfsServer: 10.240.x.x   │                       │                     │
     │                      │                         │                       │                     │
     │                      │ gRPC: EnsureTunnel      │                       │                     │
     │                      │ {volumeID, nfsServer}   │                       │                     │
     │                      │ via /csi/tunnel-mgr.sock│                       │                     │
     │                      ├────────────────────────>│                       │                     │
     │                      │                         │                       │                     │
     │                      │                         │ Check tunnel map      │                     │
     │                      │                         │ (Not found - new)     │                     │
     │                      │                         │                       │                     │
     │                      │                         │ Allocate port         │                     │
     │                      │                         │ Hash(vol-abc123)      │                     │
     │                      │                         │ → Port: 20574         │                     │
     │                      │                         │                       │                     │
     │                      │                         │ Generate config       │                     │
     │                      │                         │ /etc/stunnel/         │                     │
     │                      │                         │ vol-abc123.conf       │                     │
     │                      │                         │                       │                     │
     │                      │                         │ Start stunnel         │                     │
     │                      │                         ├──────────────────────>│                     │
     │                      │                         │                       │                     │
     │                      │                         │                       │ Establish TLS       │
     │                      │                         │                       │ connection          │
     │                      │                         │                       ├────────────────────>│
     │                      │                         │                       │                     │
     │                      │                         │                       │ TLS handshake OK    │
     │                      │                         │                       │<────────────────────┤
     │                      │                         │                       │                     │
     │                      │                         │ Save metadata         │                     │
     │                      │                         │ vol-abc123.meta       │                     │
     │                      │                         │ {port:20574,          │                     │
     │                      │                         │  refCount:1}          │                     │
     │                      │                         │                       │                     │
     │                      │ Response:               │                       │                     │
     │                      │ {port:20574,            │                       │                     │
     │                      │  refCount:1}            │                       │                     │
     │                      │<────────────────────────┤                       │                     │
     │                      │                         │                       │                     │
     │                      │ Mount NFS4              │                       │                     │
     │                      │ 127.0.0.1:/export       │                       │                     │
     │                      │ port=20574              │                       │                     │
     │                      │ → /var/lib/kubelet/...  │                       │                     │
     │                      │                         │                       │                     │
     │ Success              │                         │                       │                     │
     │<─────────────────────┤                         │                       │                     │
     │                      │                         │                       │                     │
     │ Pod can access       │                         │                       │                     │
     │ volume via NFS       │                         │                       │                     │
     │                      │                         │                       │                     │
```

### 2. Volume Mount Flow - Existing Tunnel (Shared)

```
┌──────────┐           ┌──────────┐           ┌─────────────┐           ┌─────────┐
│ Kubelet  │           │   CSI    │           │   Tunnel    │           │ Stunnel │
│          │           │  Node    │           │   Manager   │           │ Process │
└────┬─────┘           └────┬─────┘           └──────┬──────┘           └────┬────┘
     │                      │                         │                       │
     │ NodePublishVolume    │                         │                       │
     │ (vol-abc123)         │                         │                       │
     ├─────────────────────>│                         │                       │
     │                      │                         │                       │
     │                      │ gRPC: EnsureTunnel      │                       │
     │                      │ {volumeID, nfsServer}   │                       │
     │                      ├────────────────────────>│                       │
     │                      │                         │                       │
     │                      │                         │ Check tunnel map      │
     │                      │                         │ (Found - exists!)     │
     │                      │                         │                       │
     │                      │                         │ Increment RefCount    │
     │                      │                         │ RefCount: 1 → 2       │
     │                      │                         │                       │
     │                      │                         │ Update metadata       │
     │                      │                         │ vol-abc123.meta       │
     │                      │                         │ {port:20574,          │
     │                      │                         │  refCount:2}          │
     │                      │                         │                       │
     │                      │ Response:               │                       │
     │                      │ {port:20574,            │                       │
     │                      │  refCount:2}            │                       │
     │                      │<────────────────────────┤                       │
     │                      │                         │                       │
     │                      │ Mount NFS4              │                       │
     │                      │ 127.0.0.1:/export       │                       │
     │                      │ port=20574 (same port!) │                       │
     │                      │                         │                       │
     │ Success              │                         │                       │
     │<─────────────────────┤                         │                       │
     │                      │                         │                       │
     │ Second pod shares    │                         │                       │
     │ same tunnel          │                         │                       │
     │                      │                         │                       │
```

### 3. Volume Unmount Flow - Shared Tunnel (RefCount > 0)

```
┌──────────┐           ┌──────────┐           ┌─────────────┐           ┌─────────┐
│ Kubelet  │           │   CSI    │           │   Tunnel    │           │ Stunnel │
│          │           │  Node    │           │   Manager   │           │ Process │
└────┬─────┘           └────┬─────┘           └──────┬──────┘           └────┬────┘
     │                      │                         │                       │
     │ NodeUnpublishVolume  │                         │                       │
     │ (vol-abc123)         │                         │                       │
     ├─────────────────────>│                         │                       │
     │                      │                         │                       │
     │                      │ Unmount NFS             │                       │
     │                      │ from filesystem         │                       │
     │                      │                         │                       │
     │                      │ gRPC: RemoveTunnel      │                       │
     │                      │ {volumeID}              │                       │
     │                      ├────────────────────────>│                       │
     │                      │                         │                       │
     │                      │                         │ Find tunnel           │
     │                      │                         │ (Found)               │
     │                      │                         │                       │
     │                      │                         │ Decrement RefCount    │
     │                      │                         │ RefCount: 2 → 1       │
     │                      │                         │                       │
     │                      │                         │ Check RefCount        │
     │                      │                         │ (Still > 0)           │
     │                      │                         │                       │
     │                      │                         │ Update metadata       │
     │                      │                         │ vol-abc123.meta       │
     │                      │                         │ {port:20574,          │
     │                      │                         │  refCount:1}          │
     │                      │                         │                       │
     │                      │                         │ Keep tunnel running   │
     │                      │                         │ (other pods using it) │
     │                      │                         │                       │
     │                      │ Response: Success       │                       │
     │                      │ (tunnel still active)   │                       │
     │                      │<────────────────────────┤                       │
     │                      │                         │                       │
     │ Success              │                         │                       │
     │<─────────────────────┤                         │                       │
     │                      │                         │                       │
```

### 4. Volume Unmount Flow - Last Mount (RefCount = 0)

```
┌──────────┐           ┌──────────┐           ┌─────────────┐           ┌─────────┐
│ Kubelet  │           │   CSI    │           │   Tunnel    │           │ Stunnel │
│          │           │  Node    │           │   Manager   │           │ Process │
└────┬─────┘           └────┬─────┘           └──────┬──────┘           └────┬────┘
     │                      │                         │                       │
     │ NodeUnpublishVolume  │                         │                       │
     │ (vol-abc123)         │                         │                       │
     ├─────────────────────>│                         │                       │
     │                      │                         │                       │
     │                      │ Unmount NFS             │                       │
     │                      │ from filesystem         │                       │
     │                      │                         │                       │
     │                      │ gRPC: RemoveTunnel      │                       │
     │                      │ {volumeID}              │                       │
     │                      ├────────────────────────>│                       │
     │                      │                         │                       │
     │                      │                         │ Find tunnel           │
     │                      │                         │ (Found)               │
     │                      │                         │                       │
     │                      │                         │ Decrement RefCount    │
     │                      │                         │ RefCount: 1 → 0       │
     │                      │                         │                       │
     │                      │                         │ Check RefCount        │
     │                      │                         │ (Now = 0, cleanup!)   │
     │                      │                         │                       │
     │                      │                         │ Stop stunnel          │
     │                      │                         ├──────────────────────>│
     │                      │                         │                       │
     │                      │                         │                       │ SIGTERM
     │                      │                         │                       │ Graceful
     │                      │                         │                       │ shutdown
     │                      │                         │                       │
     │                      │                         │ Release port (20574)  │
     │                      │                         │ Mark as available     │
     │                      │                         │                       │
     │                      │                         │ Delete config         │
     │                      │                         │ vol-abc123.conf       │
     │                      │                         │                       │
     │                      │                         │ Delete metadata       │
     │                      │                         │ vol-abc123.meta       │
     │                      │                         │                       │
     │                      │                         │ Remove from map       │
     │                      │                         │ tunnels[vol-abc123]   │
     │                      │                         │                       │
     │                      │ Response: Success       │                       │
     │                      │ (tunnel stopped)        │                       │
     │                      │<────────────────────────┤                       │
     │                      │                         │                       │
     │ Success              │                         │                       │
     │<─────────────────────┤                         │                       │
     │                      │                         │                       │
```

### 5. Recovery Flow After Pod Restart

```
┌─────────────┐           ┌─────────────┐           ┌─────────────┐           ┌─────────┐
│   Tunnel    │           │  Metadata   │           │    /proc/   │           │ Stunnel │
│   Manager   │           │   Storage   │           │   mounts    │           │ Process │
└──────┬──────┘           └──────┬──────┘           └──────┬──────┘           └────┬────┘
       │                         │                         │                       │
       │ Pod Starts              │                         │                       │
       │                         │                         │                       │
       │ Scan for metadata       │                         │                       │
       ├────────────────────────>│                         │                       │
       │                         │                         │                       │
       │ List *.meta files       │                         │                       │
       │<────────────────────────┤                         │                       │
       │                         │                         │                       │
       │ For each metadata file: │                         │                       │
       │                         │                         │                       │
       │ Read vol-abc123.meta    │                         │                       │
       ├────────────────────────>│                         │                       │
       │                         │                         │                       │
       │ {volumeID, port:20574,  │                         │                       │
       │  nfsServer, refCount:2} │                         │                       │
       │<────────────────────────┤                         │                       │
       │                         │                         │                       │
       │ Read /proc/mounts       │                         │                       │
       ├─────────────────────────────────────────────────>│                       │
       │                         │                         │                       │
       │ Count NFS mounts        │                         │                       │
       │ using port 20574        │                         │                       │
       │                         │                         │                       │
       │ Extract pod UIDs        │                         │                       │
       │ Validate pod dirs exist │                         │                       │
       │                         │                         │                       │
       │ actualMountCount = 2    │                         │                       │
       │<─────────────────────────────────────────────────┤                       │
       │                         │                         │                       │
       │ Compare RefCounts       │                         │                       │
       │ Saved: 2, Actual: 2     │                         │                       │
       │ (Match - OK!)           │                         │                       │
       │                         │                         │                       │
       │ Restart stunnel         │                         │                       │
       │ with port 20574         │                         │                       │
       ├─────────────────────────────────────────────────────────────────────────>│
       │                         │                         │                       │
       │                         │                         │                       │ Start
       │                         │                         │                       │ process
       │                         │                         │                       │
       │ Update metadata         │                         │                       │
       │ (if RefCount corrected) │                         │                       │
       ├────────────────────────>│                         │                       │
       │                         │                         │                       │
       │ Log: Recovered tunnel   │                         │                       │
       │ vol-abc123, port 20574  │                         │                       │
       │ refCount: 2             │                         │                       │
       │                         │                         │                       │
       │ Continue with next      │                         │                       │
       │ metadata file...        │                         │                       │
       │                         │                         │                       │
       │ All tunnels recovered   │                         │                       │
       │                         │                         │                       │
       │ Start gRPC Server       │                         │                       │
       │ Listen on socket        │                         │                       │
       │                         │                         │                       │
       │ Ready for requests      │                         │                       │
       │                         │                         │                       │
```

### 6. Recovery Flow - Stale Metadata Cleanup

```
┌─────────────┐           ┌─────────────┐           ┌─────────────┐
│   Tunnel    │           │  Metadata   │           │    /proc/   │
│   Manager   │           │   Storage   │           │   mounts    │
└──────┬──────┘           └──────┬──────┘           └──────┬──────┘
       │                         │                         │
       │ Read vol-xyz789.meta    │                         │
       ├────────────────────────>│                         │
       │                         │                         │
       │ {volumeID, port:20575,  │                         │
       │  nfsServer, refCount:1} │                         │
       │<────────────────────────┤                         │
       │                         │                         │
       │ Read /proc/mounts       │                         │
       ├─────────────────────────────────────────────────>│
       │                         │                         │
       │ Count NFS mounts        │                         │
       │ using port 20575        │                         │
       │                         │                         │
       │ actualMountCount = 0    │                         │
       │ (No active mounts!)     │                         │
       │<─────────────────────────────────────────────────┤
       │                         │                         │
       │ Stale metadata detected │                         │
       │ (Pod was deleted but    │                         │
       │  metadata remained)     │                         │
       │                         │                         │
       │ Delete metadata file    │                         │
       ├────────────────────────>│                         │
       │                         │                         │
       │ Release port 20575      │                         │
       │ Mark as available       │                         │
       │                         │                         │
       │ Log: Cleaned stale      │                         │
       │ tunnel vol-xyz789       │                         │
       │                         │                         │
```

### 7. Port Allocation Flow

```
┌─────────────┐           ┌──────────────┐           ┌─────────────┐
│   Tunnel    │           │     Port     │           │   System    │
│   Manager   │           │  Allocator   │           │  (netstat)  │
└──────┬──────┘           └──────┬───────┘           └──────┬──────┘
       │                         │                          │
       │ AllocatePort            │                          │
       │ (vol-abc123)            │                          │
       ├────────────────────────>│                          │
       │                         │                          │
       │                         │ Hash volumeID            │
       │                         │ FNV-1a(vol-abc123)       │
       │                         │ = 0x1A2B3C4D             │
       │                         │                          │
       │                         │ Calculate preferred port │
       │                         │ 20000 + (hash % 10000)   │
       │                         │ = 20574                  │
       │                         │                          │
       │                         │ Check bitmap             │
       │                         │ allocated[20574]?        │
       │                         │ (No - available)         │
       │                         │                          │
       │                         │ Check system             │
       │                         ├─────────────────────────>│
       │                         │                          │
       │                         │ Port 20574 in use?       │
       │                         │<─────────────────────────┤
       │                         │ (No - free)              │
       │                         │                          │
       │                         │ Mark in bitmap           │
       │                         │ allocated[20574] = true  │
       │                         │                          │
       │ Port: 20574             │                          │
       │<────────────────────────┤                          │
       │                         │                          │
       │ Success                 │                          │
       │                         │                          │
```

### 8. Port Allocation - Collision Handling

```
┌─────────────┐           ┌──────────────┐           ┌─────────────┐
│   Tunnel    │           │     Port     │           │   System    │
│   Manager   │           │  Allocator   │           │  (netstat)  │
└──────┬──────┘           └──────┬───────┘           └──────┬──────┘
       │                         │                          │
       │ AllocatePort            │                          │
       │ (vol-xyz789)            │                          │
       ├────────────────────────>│                          │
       │                         │                          │
       │                         │ Hash volumeID            │
       │                         │ = 20574 (collision!)     │
       │                         │                          │
       │                         │ Check bitmap             │
       │                         │ allocated[20574]?        │
       │                         │ (Yes - in use)           │
       │                         │                          │
       │                         │ Linear probe: 20574++    │
       │                         │ Try port 20575           │
       │                         │                          │
       │                         │ Check bitmap             │
       │                         │ allocated[20575]?        │
       │                         │ (No - available)         │
       │                         │                          │
       │                         │ Check system             │
       │                         ├─────────────────────────>│
       │                         │                          │
       │                         │ Port 20575 in use?       │
       │                         │<─────────────────────────┤
       │                         │ (No - free)              │
       │                         │                          │
       │                         │ Mark in bitmap           │
       │                         │ allocated[20575] = true  │
       │                         │                          │
       │ Port: 20575             │                          │
       │<────────────────────────┤                          │
       │                         │                          │
```

### 9. Health Check and Auto-Restart

```
┌──────────────┐           ┌─────────────┐           ┌─────────┐
│    Health    │           │   Tunnel    │           │ Stunnel │
│    Check     │           │   Manager   │           │ Process │
│   (Timer)    │           │             │           │         │
└──────┬───────┘           └──────┬──────┘           └────┬────┘
       │                          │                       │
       │ Every 30 seconds         │                       │
       │                          │                       │
       │ Check tunnel health      │                       │
       ├─────────────────────────>│                       │
       │                          │                       │
       │                          │ Check process status  │
       │                          ├──────────────────────>│
       │                          │                       │
       │                          │ Process dead!         │
       │                          │<──────────────────────┤
       │                          │                       │
       │                          │ State: Running → Failed
       │                          │                       │
       │                          │ Preserve RefCount & Port
       │                          │ (Don't release)       │
       │                          │                       │
       │                          │ Auto-restart attempt  │
       │                          │ (Retry #1 - immediate)│
       │                          │                       │
       │                          │ Restart stunnel       │
       │                          ├──────────────────────>│
       │                          │                       │
       │                          │                       │ Start
       │                          │                       │ process
       │                          │                       │
       │                          │ Process running       │
       │                          │<──────────────────────┤
       │                          │                       │
       │                          │ State: Failed → Running
       │                          │                       │
       │ Health check OK          │                       │
       │<─────────────────────────┤                       │
       │                          │                       │
```

### 10. Reference Counting Lifecycle

```
┌──────────┐           ┌─────────────┐           ┌─────────┐
│   Pod 1  │           │   Tunnel    │           │ Stunnel │
│          │           │   Manager   │           │ Process │
└────┬─────┘           └──────┬──────┘           └────┬────┘
     │                        │                       │
     │ Mount vol-abc123       │                       │
     ├───────────────────────>│                       │
     │                        │ Create tunnel         │
     │                        │ RefCount = 1          │
     │                        ├──────────────────────>│
     │                        │                       │
     │                        │                       │ Start
     │                        │                       │
     │ Success                │                       │
     │<───────────────────────┤                       │
     │                        │                       │
     
┌──────────┐                  │                       │
│   Pod 2  │                  │                       │
│          │                  │                       │
└────┬─────┘                  │                       │
     │                        │                       │
     │ Mount vol-abc123       │                       │
     │ (same volume!)         │                       │
     ├───────────────────────>│                       │
     │                        │ Tunnel exists         │
     │                        │ RefCount: 1 → 2       │
     │                        │ (No new process)      │
     │                        │                       │
     │ Success (shared)       │                       │
     │<───────────────────────┤                       │
     │                        │                       │
     
┌──────────┐                  │                       │
│   Pod 1  │                  │                       │
└────┬─────┘                  │                       │
     │                        │                       │
     │ Unmount vol-abc123     │                       │
     ├───────────────────────>│                       │
     │                        │ RefCount: 2 → 1       │
     │                        │ (Keep running)        │
     │                        │                       │
     │ Success                │                       │
     │<───────────────────────┤                       │
     │                        │                       │
     
┌──────────┐                  │                       │
│   Pod 2  │                  │                       │
└────┬─────┘                  │                       │
     │                        │                       │
     │ Unmount vol-abc123     │                       │
     ├───────────────────────>│                       │
     │                        │ RefCount: 1 → 0       │
     │                        │ (Last mount!)         │
     │                        │                       │
     │                        │ Stop stunnel          │
     │                        ├──────────────────────>│
     │                        │                       │
     │                        │                       │ SIGTERM
     │                        │                       │ Shutdown
     │                        │                       │
     │                        │ Release port          │
     │                        │ Delete config         │
     │                        │ Delete metadata       │
     │                        │                       │
     │ Success                │                       │
     │<───────────────────────┤                       │
     │                        │                       │
```

### 11. gRPC Communication Architecture

```
┌──────────┐           ┌──────────┐           ┌─────────────┐           ┌─────────┐
│   CSI    │           │  gRPC    │           │    gRPC     │           │ Tunnel  │
│  Driver  │           │  Client  │           │   Server    │           │ Manager │
└────┬─────┘           └────┬─────┘           └──────┬──────┘           └────┬────┘
     │                      │                         │                       │
     │ EnsureTunnel()       │                         │                       │
     ├─────────────────────>│                         │                       │
     │                      │                         │                       │
     │                      │ Connect to socket       │                       │
     │                      │ /csi/tunnel-manager.sock│                       │
     │                      ├────────────────────────>│                       │
     │                      │                         │                       │
     │                      │ gRPC Call               │                       │
     │                      │ EnsureTunnel RPC        │                       │
     │                      │ {volumeID, nfsServer}   │                       │
     │                      ├────────────────────────>│                       │
     │                      │                         │                       │
     │                      │                         │ Invoke handler        │
     │                      │                         ├──────────────────────>│
     │                      │                         │                       │
     │                      │                         │                       │ Process
     │                      │                         │                       │ request
     │                      │                         │                       │
     │                      │                         │ TunnelInfo            │
     │                      │                         │<──────────────────────┤
     │                      │                         │                       │
     │                      │ gRPC Response           │                       │
     │                      │ {port, refCount}        │                       │
     │                      │<────────────────────────┤                       │
     │                      │                         │                       │
     │ TunnelInfo           │                         │                       │
     │<─────────────────────┤                         │                       │
     │                      │                         │                       │
     │ Use port for mount   │                         │                       │
     │                      │                         │                       │
```

---

## Security Model

### TLS Configuration

**Certificate Verification:**
- `verify = 2`: Require and verify peer certificate
- `CAfile`: System CA bundle for certificate validation
- Certificates managed by IBM Cloud infrastructure

**Encryption:**
- TLS 1.2+ for all connections
- Strong cipher suites enforced by stunnel
- No plaintext NFS traffic on network

### Network Security

```
Pod → 127.0.0.1:20XXX → Stunnel → TLS → RFS Server
      (Loopback)        (Encrypt)  (Network)
```

**Benefits:**
- NFS traffic never leaves node unencrypted
- Loopback interface (127.0.0.1) prevents network exposure
- Each tunnel isolated by unique port
- No cross-pod tunnel sharing (security boundary)

### Privilege Requirements

**Tunnel Manager Container:**
- `privileged: true` - Required for:
  - Managing stunnel processes
  - Accessing host /proc/mounts
  - Reading host certificates
- `runAsUser: 0` - Required for port binding < 1024 (if needed)

**CSI Node Driver Container:**
- `privileged: true` - Required for:
  - Mounting filesystems
  - Accessing /dev
  - Managing kubelet directories

---

## High Availability

### Failure Scenarios

#### 1. Stunnel Process Crash

```
┌──────────────┐     ┌────────────────┐     ┌─────────────────┐     ┌────────────┐
│ Health Check │     │ Tunnel Manager │     │ Stunnel Process │     │ RFS Server │
└──────┬───────┘     └────────┬───────┘     └────────┬────────┘     └─────┬──────┘
       │                      │                       │                    │
       │ 1. Check process     │                       │                    │
       │    status            │                       │                    │
       ├─────────────────────────────────────────────►│                    │
       │                      │                       │                    │
       │ 2. Process dead      │                       │                    │
       │◄─────────────────────────────────────────────┤                    │
       │                      │                       │                    │
       │ 3. Report failure    │                       │                    │
       ├─────────────────────►│                       │                    │
       │                      │                       │                    │
       │                      │ 4. Preserve RefCount  │                    │
       │                      │    & Port             │                    │
       │                      │ ─┐                    │                    │
       │                      │  │                    │                    │
       │                      │ ◄┘                    │                    │
       │                      │                       │                    │
       │                      │ 5. Restart stunnel    │                    │
       │                      ├──────────────────────►│                    │
       │                      │                       │                    │
       │                      │                       │ 6. Re-establish    │
       │                      │                       │    TLS connection  │
       │                      │                       ├───────────────────►│
       │                      │                       │                    │
       │                      │                       │ 7. Connected       │
       │                      │                       │◄───────────────────┤
       │                      │                       │                    │
       │                      │ 8. Running            │                    │
       │                      │◄──────────────────────┤                    │
       │                      │                       │                    │
       │                      │ 9. Update state to    │                    │
       │                      │    Running            │                    │
       │                      │ ─┐                    │                    │
       │                      │  │                    │                    │
       │                      │ ◄┘                    │                    │
       │                      │                       │                    │

**Recovery Steps:**
1. Health check detects stunnel process is dead
2. Reports failure to Tunnel Manager
3. Tunnel Manager preserves RefCount and allocated port
4. Restarts stunnel with same configuration
5. Stunnel re-establishes TLS connection to RFS server
6. Tunnel returns to Running state
7. No disruption to existing mount references
```

#### 2. Tunnel Manager Pod Restart

**Recovery Process:**
1. Load metadata from persistent storage (`/etc/stunnel/*.meta`)
2. Verify actual mounts from `/proc/mounts`
3. Restart tunnels with verified RefCounts
4. Clean up stale metadata

**Why /proc/mounts?**
- ✅ Never hangs (kernel data structure)
- ✅ Accurate even when NFS server is down
- ✅ Faster than `df` or `mount` commands
- ✅ Works in degraded states

#### 3. NFS Server Unreachable

**Behavior:**
- Stunnel keeps running (connection retry)
- NFS mount becomes unresponsive
- Tunnel manager continues health checks
- No automatic cleanup (preserve user data)

**Manual Recovery:**
- Admin must fix network/server issues
- Tunnels automatically reconnect when server returns

### Monitoring

**Health Metrics:**
```go
type HealthStatus struct {
    TotalTunnels    int
    RunningTunnels  int
    FailedTunnels   int
    PortsAllocated  int
    LastRecovery    time.Time
}
```

**Logging:**
- Structured logging with zap
- Log levels: DEBUG, INFO, WARN, ERROR
- Key events logged:
  - Tunnel creation/deletion
  - RefCount changes
  - Health check failures
  - Recovery operations

---

## Performance Considerations

### Resource Usage

**Per Tunnel:**
- Memory: ~5-10 MB (stunnel process)
- CPU: Minimal (encryption overhead)
- Disk: ~2 KB (config + metadata)

**Scalability:**
- Max tunnels per node: ~10,000 (port range limit)
- Typical usage: 10-100 tunnels per node
- No performance degradation with multiple tunnels

### Optimization

**Port Allocation:**
- O(1) average case (hash-based)
- O(n) worst case (linear probing)
- Bitmap for fast lookup

**Metadata Operations:**
- Async writes (non-blocking)
- Batch recovery on startup
- Minimal disk I/O during normal operation

---

## References

- [Stunnel Documentation](https://www.stunnel.org/docs.html)
- [gRPC Go Documentation](https://grpc.io/docs/languages/go/)
- [Kubernetes CSI Specification](https://kubernetes-csi.github.io/docs/)
- [IBM Cloud VPC File Storage](https://cloud.ibm.com/docs/vpc?topic=vpc-file-storage-vpc-about)

---

## References

