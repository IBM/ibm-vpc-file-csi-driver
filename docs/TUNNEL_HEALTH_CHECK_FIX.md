# Critical Bug Fix: Tunnel Health Check with NFS Server Connectivity

## Problem Identified

### Symptom
When the NFS server is unreachable or not accepting connections, the tunnel manager incorrectly marks the tunnel as "healthy" and keeps incrementing the refCount on subsequent `NodePublishVolume` calls, even though the first mount is hanging.

### Log Evidence
```
2026.04.10 09:55:04 LOG5[0]: Service [nfs-r022-...] accepted connection from 127.0.0.1:37465
{"level":"info","msg":"Tunnel created successfully","volumeID":"r022-...","port":29565,"refCount":1}

2026.04.10 09:55:14 LOG3[0]: s_connect: s_poll_wait 10.244.0.9:20049: TIMEOUTconnect exceeded
2026.04.10 09:55:14 LOG3[0]: No more addresses to connect
2026.04.10 09:55:14 LOG5[0]: Connection reset: 0 byte(s) sent to TLS, 0 byte(s) sent to socket

... (multiple timeout errors) ...

2026.04.10 09:58:04 {"level":"info","msg":"Tunnel already exists and is healthy, incremented refcount","refCount":2}
```

### Root Cause

The `checkTunnelHealth()` function only verified:
1. ✅ Tunnel process is running
2. ✅ Local port (127.0.0.1:29565) is listening

But it did NOT verify:
3. ❌ **NFS server is actually reachable**

This caused the tunnel to be marked as "healthy" even when stunnel couldn't connect to the NFS server, leading to:
- Incorrect refCount increments
- Multiple pods trying to use a broken tunnel
- Mount operations hanging indefinitely
- Resource leaks

## The Fix

### Before (Buggy Code)

```go
func (m *Manager) checkTunnelHealth(t *Tunnel) bool {
    if t.State != StateRunning {
        return false
    }

    // Check if process is still running
    if t.Cmd == nil || t.Cmd.Process == nil {
        return false
    }

    // Check if port is still listening
    addr := fmt.Sprintf("127.0.0.1:%d", t.LocalPort)
    conn, err := net.DialTimeout("tcp", addr, time.Second)
    if err != nil {
        return false
    }
    conn.Close()

    return true  // ❌ WRONG: Tunnel process running ≠ NFS server reachable
}
```

### After (Fixed Code)

```go
func (m *Manager) checkTunnelHealth(t *Tunnel) bool {
    if t.State != StateRunning {
        return false
    }

    // Check if process is still running
    if t.Cmd == nil || t.Cmd.Process == nil {
        return false
    }

    // Check if port is still listening
    addr := fmt.Sprintf("127.0.0.1:%d", t.LocalPort)
    conn, err := net.DialTimeout("tcp", addr, time.Second)
    if err != nil {
        return false
    }
    conn.Close()

    // ✅ CRITICAL: Check if NFS server is actually reachable
    if !m.checkNFSServerConnectivity(t) {
        m.logger.Warn("Tunnel process running but NFS server unreachable",
            zap.String("volumeID", t.VolumeID),
            zap.String("nfsServer", t.RemoteAddr),
            zap.Int("port", t.LocalPort))
        return false
    }

    return true
}

// New function to verify NFS server connectivity
func (m *Manager) checkNFSServerConnectivity(t *Tunnel) bool {
    // Try to connect directly to the NFS server
    nfsAddr := fmt.Sprintf("%s:%d", t.RemoteAddr, m.nfsPort)
    conn, err := net.DialTimeout("tcp", nfsAddr, 2*time.Second)
    if err != nil {
        m.logger.Debug("NFS server connectivity check failed",
            zap.String("volumeID", t.VolumeID),
            zap.String("nfsServer", nfsAddr),
            zap.Error(err))
        return false
    }
    conn.Close()
    return true
}
```

## Behavior Changes

### Before Fix

```
First NodePublishVolume:
  ├─ Create tunnel (refCount=1)
  ├─ Stunnel process starts
  ├─ Stunnel accepts connections on 127.0.0.1:29565
  ├─ Stunnel FAILS to connect to NFS server (timeout)
  └─ Mount hangs (NFS client waiting)

Second NodePublishVolume (same volume):
  ├─ Check tunnel health
  │  ├─ Process running? ✅ Yes
  │  ├─ Port listening? ✅ Yes
  │  └─ Health check PASSES ❌ WRONG!
  ├─ Increment refCount (1 → 2) ❌ WRONG!
  └─ Mount hangs again

Result: RefCount keeps growing, multiple hanging mounts
```

### After Fix

```
First NodePublishVolume:
  ├─ Create tunnel (refCount=1)
  ├─ Stunnel process starts
  ├─ Stunnel accepts connections on 127.0.0.1:29565
  ├─ Stunnel FAILS to connect to NFS server (timeout)
  └─ Mount hangs (NFS client waiting)

Second NodePublishVolume (same volume):
  ├─ Check tunnel health
  │  ├─ Process running? ✅ Yes
  │  ├─ Port listening? ✅ Yes
  │  ├─ NFS server reachable? ❌ No (timeout)
  │  └─ Health check FAILS ✅ CORRECT!
  ├─ Restart tunnel (attempt recovery)
  └─ If still fails, return error to CSI

Result: Proper error handling, no incorrect refCount increments
```

## Impact

### Issues Fixed

1. **Prevents RefCount Inflation**
   - RefCount only increments when tunnel is truly healthy
   - No more ghost references to broken tunnels

2. **Faster Failure Detection**
   - Detects NFS server issues immediately
   - Doesn't wait for mount to hang

3. **Better Error Reporting**
   - Clear logs when NFS server is unreachable
   - Helps operators identify network/server issues

4. **Proper Recovery**
   - Triggers tunnel restart when NFS server becomes unreachable
   - Attempts to re-establish connection

### Scenarios Handled

| Scenario | Before Fix | After Fix |
|----------|-----------|-----------|
| NFS server down | RefCount inflates, mounts hang | Health check fails, error returned |
| Network partition | RefCount inflates, mounts hang | Health check fails, tunnel restarted |
| Firewall blocking NFS | RefCount inflates, mounts hang | Health check fails, error logged |
| NFS server slow | RefCount inflates, mounts hang | Health check fails after 2s timeout |

## Testing

### Test Case 1: NFS Server Unreachable

```bash
# 1. Create PVC with RFS + EIT
kubectl apply -f test-pvc.yaml

# 2. Block NFS server port (simulate network issue)
# On NFS server node:
sudo iptables -A INPUT -p tcp --dport 20049 -j DROP

# 3. Try to mount (first pod)
kubectl apply -f test-pod.yaml

# Expected: Tunnel created, mount hangs, logs show NFS timeout

# 4. Try to mount again (second pod)
kubectl apply -f test-pod2.yaml

# Expected (BEFORE FIX): RefCount=2, both mounts hang
# Expected (AFTER FIX): Health check fails, tunnel restart attempted, error returned

# 5. Check logs
kubectl logs -n kube-system <csi-node-pod> -c tunnel-manager

# Should see:
# "Tunnel process running but NFS server unreachable"
# "NFS server connectivity check failed"
```

### Test Case 2: NFS Server Recovery

```bash
# 1. Start with NFS server blocked (from Test Case 1)

# 2. Unblock NFS server
sudo iptables -D INPUT -p tcp --dport 20049 -j DROP

# 3. Wait for health check cycle (30 seconds)

# 4. Try to mount
kubectl apply -f test-pod3.yaml

# Expected: Health check passes, tunnel works, mount succeeds
```

### Test Case 3: Concurrent Mounts with Unreachable Server

```bash
# 1. Block NFS server
sudo iptables -A INPUT -p tcp --dport 20049 -j DROP

# 2. Create multiple pods simultaneously
for i in {1..5}; do
  kubectl apply -f test-pod-$i.yaml &
done

# Expected (BEFORE FIX): All 5 increment refCount, all hang
# Expected (AFTER FIX): First creates tunnel, others fail health check, errors returned
```

## Monitoring

### Key Log Messages

**Healthy Tunnel:**
```json
{"level":"info","msg":"Tunnel already exists and is healthy, incremented refcount","refCount":2}
```

**Unhealthy Tunnel (After Fix):**
```json
{"level":"warn","msg":"Tunnel process running but NFS server unreachable","volumeID":"r022-...","nfsServer":"10.244.0.9"}
{"level":"debug","msg":"NFS server connectivity check failed","nfsServer":"10.244.0.9:20049","error":"dial tcp 10.244.0.9:20049: i/o timeout"}
```

### Metrics to Monitor

1. **Tunnel Health Check Failures**
   - Indicates NFS server connectivity issues
   - Should trigger alerts

2. **RefCount Accuracy**
   - Compare refCount with actual mount count
   - Should match after fix

3. **Mount Success Rate**
   - Should improve with proper error handling
   - Faster failure detection

## Deployment

### Rollout Strategy

1. **Deploy to Dev/Test First**
   - Verify health checks work correctly
   - Test with unreachable NFS server

2. **Monitor Logs**
   - Watch for new warning messages
   - Verify no false positives

3. **Gradual Production Rollout**
   - Deploy to one cluster at a time
   - Monitor for 24 hours before next cluster

### Rollback Plan

If issues occur:
```bash
# Revert to previous version
kubectl set image daemonset/ibm-vpc-file-csi-node \
  tunnel-manager=<previous-image> \
  -n kube-system
```

## Related Issues

- **Refcount Leak on Reboot** - Fixed separately
- **Orphaned Mount Detection** - Uses similar connectivity checks
- **Timeout Protection** - Prevents indefinite hangs

## Conclusion

This fix ensures that tunnel health checks verify **end-to-end connectivity**, not just local process status. This prevents incorrect refCount increments and provides better error handling when NFS servers are unreachable.

**Key Principle:** A tunnel is only "healthy" if it can actually reach the NFS server, not just if the stunnel process is running.