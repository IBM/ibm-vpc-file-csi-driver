# Stunnel RefCount Fix: Missing Kubelet Volume Mount

## Problem Discovered

The tunnel-manager container was unable to detect active mounts during recovery, causing it to incorrectly clean up tunnels that were still in use.

### Symptoms

```bash
# Mount exists in /proc/mounts
sh-5.1# cat /proc/mounts | grep 127.0.0.1 | grep nfs4
127.0.0.1:/... /var/lib/kubelet/pods/18bfdb1f-731f-4f8b-9e1f-aa4a7fefb896/... nfs4 ...port=20574...
127.0.0.1:/... /var/data/kubelet/pods/18bfdb1f-731f-4f8b-9e1f-aa4a7fefb896/... nfs4 ...port=20574...

# But tunnel-manager logs show 0 mounts found
{"msg":"Verified mount count from /proc/mounts","actualMountCount":0}
{"msg":"No active mounts found in /proc/mounts, cleaning up stale tunnel metadata"}
```

### Root Cause

The `tunnel-manager` container was missing the `kubelet-data-dir` volume mount, preventing it from accessing `/var/lib/kubelet/pods/` to validate pod directories.

**Code Flow:**
1. `getActiveMountCount()` reads `/proc/mounts` ✅
2. Extracts pod UID from mount path ✅
3. Tries to check if pod directory exists: `os.Stat("/var/lib/kubelet/pods/<pod-uid>")` ❌
4. **FAILS** because `/var/lib/kubelet` is not mounted in the container
5. Treats mount as orphaned and doesn't count it ❌
6. Returns `actualMountCount=0` ❌
7. Recovery logic cleans up the tunnel ❌

## Solution

### 1. Add Volume Mount to tunnel-manager Container

**File:** `deploy/kubernetes/manifests/node-server.yaml`

```yaml
- name: tunnel-manager
  volumeMounts:
    - name: plugin-dir
      mountPath: /var/lib/kubelet/plugins/vpc.file.csi.ibm.io/
    - name: stunnel-dir
      mountPath: /etc/stunnel
    # ADD THIS:
    - name: kubelet-data-dir
      mountPath: /var/lib/kubelet
      mountPropagation: "HostToContainer"
```

**Why `HostToContainer`?**
- Allows the container to see new mounts created on the host
- Read-only propagation is sufficient (we only check directory existence)
- More secure than `Bidirectional` (which allows container to create mounts visible to host)

### 2. Handle Custom Kubelet Paths

Some systems use custom kubelet paths (e.g., `/var/data/kubelet` instead of `/var/lib/kubelet`).

**Updated Code:** `pkg/tunnel/manager.go`

```go
// Extract the kubelet base path from the actual mount point
// instead of hardcoding /var/lib/kubelet
kubeletBasePath := mountPoint[:idx] // Everything before "/pods/"
podDir := filepath.Join(kubeletBasePath, "pods", podUID)

// Use timeout-protected stat to prevent hanging on unstable/hung mounts
_, statErr := statWithTimeout(podDir, 2*time.Second)

if statErr == nil {
    // Pod directory exists, this is a valid active mount
    validPodUIDs[podUID] = true
} else if statErr == context.DeadlineExceeded {
    // Stat operation timed out - likely hung mount
    // Treat as orphaned to avoid blocking recovery
    m.logger.Warn("Pod directory stat timed out (likely hung mount)")
}
```

**Benefits:**
- Works with any kubelet path (`/var/lib/kubelet`, `/var/data/kubelet`, etc.)
- Handles symlinks correctly (e.g., `/var/lib/kubelet` → `/var/data/kubelet`)
- **Timeout protection** prevents hanging on unstable/hung NFS mounts
- More robust and portable across different Kubernetes distributions

### 3. Timeout Protection for Hung Mounts

**Problem:** If an NFS mount is hung or unstable, `os.Stat()` on paths within that mount can hang indefinitely, blocking the entire recovery process.

**Solution:** Implemented `statWithTimeout()` function that runs `os.Stat()` in a goroutine with a 2-second timeout.

```go
func statWithTimeout(path string, timeout time.Duration) (os.FileInfo, error) {
    type result struct {
        info os.FileInfo
        err  error
    }
    
    resultChan := make(chan result, 1)
    
    go func() {
        info, err := os.Stat(path)
        resultChan <- result{info: info, err: err}
    }()
    
    select {
    case res := <-resultChan:
        return res.info, res.err
    case <-time.After(timeout):
        return nil, context.DeadlineExceeded
    }
}
```

**Why 2 seconds?**
- Local filesystem access should complete in milliseconds
- 2 seconds is generous for slow systems
- Prevents indefinite hangs while allowing legitimate slow operations
- If timeout occurs, mount is likely hung and should be treated as orphaned

**Behavior on Timeout:**
- Logs warning: "Pod directory stat timed out (likely hung mount)"
- Treats mount as orphaned (doesn't count it)
- Allows recovery to continue without blocking
- Prevents cascading failures from hung mounts

## Testing

### Before Fix

```bash
# Tunnel incorrectly cleaned up
{"msg":"No active mounts found in /proc/mounts, cleaning up stale tunnel metadata"}
{"msg":"Tunnel recovery completed","recovered":0,"cleaned":2}

# Pod loses connection to volume
# Application errors due to missing mount
```

### After Fix

```bash
# Mounts correctly detected
{"msg":"Found valid mount for existing pod","podUID":"18bfdb1f-...","podDir":"/var/lib/kubelet/pods/18bfdb1f-..."}
{"msg":"Verified mount count from /proc/mounts","actualMountCount":1}
{"msg":"Tunnel recovery completed","recovered":1,"cleaned":0}

# Tunnel stays active
# Application continues working
```

### Verification Commands

```bash
# 1. Check if tunnel-manager can access kubelet directory
kubectl exec -n kube-system <csi-pod> -c tunnel-manager -- ls -la /var/lib/kubelet/pods/

# 2. Verify mount detection works
kubectl delete pod -n kube-system <csi-pod> --force --grace-period=0
kubectl logs -n kube-system <new-csi-pod> -c tunnel-manager | grep "Found valid mount"

# 3. Check recovery stats
kubectl logs -n kube-system <csi-pod> -c tunnel-manager | grep "Tunnel recovery completed"
# Should show: recovered=1, cleaned=0 (not recovered=0, cleaned=1)
```

## Impact

### Before Fix
- ❌ Tunnels incorrectly cleaned up on container restart
- ❌ Active mounts lost connection
- ❌ Application errors and pod restarts
- ❌ Data access interruption

### After Fix
- ✅ Tunnels correctly preserved on container restart
- ✅ Active mounts maintain connection
- ✅ No application errors
- ✅ Zero downtime during recovery

## Deployment

### Required Changes

1. **Update DaemonSet manifest:**
   ```bash
   kubectl apply -f deploy/kubernetes/manifests/node-server.yaml
   ```

2. **Rolling restart of CSI node pods:**
   ```bash
   kubectl rollout restart daemonset/ibm-vpc-file-csi-node -n kube-system
   ```

3. **Monitor recovery logs:**
   ```bash
   kubectl logs -n kube-system -l app=ibm-vpc-file-csi-node -c tunnel-manager --tail=50 -f
   ```

### Rollback Plan

If issues occur, revert the DaemonSet:
```bash
kubectl rollout undo daemonset/ibm-vpc-file-csi-node -n kube-system
```

## Related Issues

This fix addresses:
1. **Node reboot refcount issue** - Now correctly detects active mounts after reboot
2. **Container restart tunnel loss** - Tunnels preserved across restarts
3. **Custom kubelet path support** - Works with any kubelet directory location
4. **Orphaned mount detection** - Correctly identifies truly orphaned mounts

## Summary

**Critical Fix:** The tunnel-manager container MUST have access to `/var/lib/kubelet/pods/` to validate pod directories and correctly count active mounts.

**Without this fix:** Recovery logic cannot distinguish between active and orphaned mounts, leading to incorrect tunnel cleanup.

**With this fix:** Recovery logic accurately detects active mounts and preserves tunnels that are still in use.