# Critical Fix: First Mount with NFS Server Down

## Problem Statement

**Critical Bug:** When the first pod attempts to mount a volume and the NFS server is unreachable, the tunnel manager creates a "healthy" tunnel with refCount=1, even though the NFS server is down. This causes:

1. ❌ Tunnel created with refCount=1
2. ❌ Mount fails (NFS server unreachable)
3. ❌ Tunnel remains in memory with refCount=1 (LEAK!)
4. ❌ Subsequent pods cannot use the tunnel (it's marked as "healthy" but broken)

## Root Cause

### Inconsistency Between Two Functions

The bug was caused by **inconsistent health checking** between initial tunnel creation and ongoing health monitoring:

#### `waitForTunnel()` - Used During Initial Creation
```go
// BEFORE FIX - Only checked local port
func (m *Manager) waitForTunnel(t *Tunnel, timeout time.Duration) error {
    for time.Now().Before(deadline) {
        conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
        if err == nil {
            conn.Close()
            return nil  // ❌ SUCCESS even if NFS server down!
        }
        time.Sleep(200 * time.Millisecond)
    }
    return fmt.Errorf("tunnel did not become ready within %v", timeout)
}
```

**Problem:** Only verified:
- ✅ Stunnel process started
- ✅ Local port listening
- ❌ **NFS server reachable** (MISSING!)

#### `checkTunnelHealth()` - Used for Existing Tunnels
```go
// Already had NFS server check
func (m *Manager) checkTunnelHealth(t *Tunnel) bool {
    // ... check process and port ...
    
    // CRITICAL: Check if NFS server is actually reachable
    if !m.checkNFSServerConnectivity(t) {
        return false  // ✅ Correctly fails if NFS server down
    }
    return true
}
```

**Correct:** Verified:
- ✅ Stunnel process running
- ✅ Local port listening
- ✅ **NFS server reachable**

### The Inconsistency

| Scenario | Function Called | NFS Check | Result |
|----------|----------------|-----------|--------|
| **First mount** (NFS down) | `waitForTunnel()` | ❌ Missing | Tunnel created (WRONG!) |
| **Second mount** (NFS down) | `checkTunnelHealth()` | ✅ Present | Error returned (CORRECT!) |

This inconsistency meant:
- First pod: Creates broken tunnel with refCount=1 (leak)
- Second pod: Correctly detects NFS server down and returns error

## The Fix

### Updated `waitForTunnel()` Function

```go
// AFTER FIX - Now consistent with checkTunnelHealth()
func (m *Manager) waitForTunnel(t *Tunnel, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    addr := fmt.Sprintf("127.0.0.1:%d", t.LocalPort)

    for time.Now().Before(deadline) {
        // First check if local tunnel port is listening
        conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
        if err == nil {
            conn.Close()
            
            // CRITICAL: Also verify NFS server is reachable (consistent with checkTunnelHealth)
            // This prevents creating "healthy" tunnels when NFS server is down on FIRST mount
            if m.checkNFSServerConnectivity(t) {
                m.logger.Info("Tunnel ready and NFS server reachable",
                    zap.String("volumeID", t.VolumeID),
                    zap.String("nfsServer", t.RemoteAddr))
                return nil  // ✅ Only succeed if BOTH tunnel AND NFS server ready
            }
            
            // Tunnel process running but NFS server unreachable - keep retrying
            m.logger.Debug("Tunnel process ready but NFS server unreachable, retrying...",
                zap.String("volumeID", t.VolumeID),
                zap.String("nfsServer", t.RemoteAddr))
        }
        time.Sleep(200 * time.Millisecond)
    }

    return fmt.Errorf("tunnel did not become ready within %v (NFS server may be unreachable)", timeout)
}
```

### Key Changes

1. **Added NFS server connectivity check** - Now consistent with `checkTunnelHealth()`
2. **Retry logic** - Keeps retrying if tunnel process ready but NFS server down
3. **Clear error message** - Indicates NFS server may be unreachable
4. **Prevents tunnel leak** - Won't create tunnel with refCount=1 if NFS server down

## Behavior After Fix

### Scenario 1: First Mount with NFS Server Down

```
Time  Event
----  -----
T+0s  Pod-1 requests mount for volume-A
T+0s  EnsureTunnel() called (tunnel doesn't exist)
T+0s  createTunnel() starts stunnel process
T+1s  Stunnel process starts, local port 30000 listening
T+1s  waitForTunnel() checks local port: ✅ OK
T+1s  waitForTunnel() checks NFS server: ❌ UNREACHABLE
T+1s  waitForTunnel() retries...
T+2s  waitForTunnel() checks local port: ✅ OK
T+2s  waitForTunnel() checks NFS server: ❌ UNREACHABLE
T+2s  waitForTunnel() retries...
...
T+10s waitForTunnel() timeout
T+10s createTunnel() fails, cleans up:
      - Stops stunnel process
      - Releases port 30000
      - Removes config file
T+10s EnsureTunnel() returns error to CSI driver
T+10s CSI driver returns error to kubelet
T+10s Pod-1 mount fails (expected)
T+10s ✅ NO TUNNEL LEAK - tunnel was never registered
```

**Result:** ✅ Correct behavior - no tunnel leak, clear error message

### Scenario 2: First Mount with NFS Server Healthy

```
Time  Event
----  -----
T+0s  Pod-1 requests mount for volume-A
T+0s  EnsureTunnel() called (tunnel doesn't exist)
T+0s  createTunnel() starts stunnel process
T+1s  Stunnel process starts, local port 30000 listening
T+1s  waitForTunnel() checks local port: ✅ OK
T+1s  waitForTunnel() checks NFS server: ✅ REACHABLE
T+1s  waitForTunnel() returns success
T+1s  Tunnel registered with refCount=1
T+1s  EnsureTunnel() returns tunnel to CSI driver
T+1s  CSI driver mounts volume successfully
T+1s  Pod-1 running with volume mounted
```

**Result:** ✅ Correct behavior - tunnel created and mount succeeds

### Scenario 3: NFS Server Recovers During Retry

```
Time  Event
----  -----
T+0s  Pod-1 requests mount for volume-A
T+0s  EnsureTunnel() called (tunnel doesn't exist)
T+0s  createTunnel() starts stunnel process
T+1s  Stunnel process starts, local port 30000 listening
T+1s  waitForTunnel() checks NFS server: ❌ UNREACHABLE
T+2s  waitForTunnel() checks NFS server: ❌ UNREACHABLE
T+3s  waitForTunnel() checks NFS server: ❌ UNREACHABLE
T+4s  NFS server comes back online
T+5s  waitForTunnel() checks NFS server: ✅ REACHABLE
T+5s  waitForTunnel() returns success
T+5s  Tunnel registered with refCount=1
T+5s  Mount succeeds
```

**Result:** ✅ Correct behavior - automatic recovery when NFS server comes back

## Consistency Guarantee

Both functions now use the **same health criteria**:

| Check | `waitForTunnel()` | `checkTunnelHealth()` |
|-------|-------------------|----------------------|
| Tunnel process running | ✅ | ✅ |
| Local port listening | ✅ | ✅ |
| NFS server reachable | ✅ | ✅ |

This ensures:
- **First mount** behaves the same as **subsequent mounts**
- **No tunnel leaks** when NFS server is down
- **Consistent error handling** across all scenarios
- **Automatic recovery** when NFS server comes back online

## Testing

### Test Case 1: First Mount with NFS Server Down
```bash
# Simulate NFS server down
iptables -A OUTPUT -p tcp --dport 2049 -d 10.240.0.5 -j DROP

# Attempt mount
kubectl apply -f test-pod.yaml

# Expected: Mount fails with clear error message
# Expected: No tunnel leak (check tunnel-manager logs)
# Expected: No orphaned stunnel processes
```

### Test Case 2: NFS Server Recovery
```bash
# Start with NFS server down
iptables -A OUTPUT -p tcp --dport 2049 -d 10.240.0.5 -j DROP

# Attempt mount (will retry for 10 seconds)
kubectl apply -f test-pod.yaml &

# After 5 seconds, restore NFS connectivity
sleep 5
iptables -D OUTPUT -p tcp --dport 2049 -d 10.240.0.5 -j DROP

# Expected: Mount succeeds after NFS server recovers
# Expected: Tunnel created with refCount=1
```

### Test Case 3: Multiple Pods with NFS Server Down
```bash
# Simulate NFS server down
iptables -A OUTPUT -p tcp --dport 2049 -d 10.240.0.5 -j DROP

# Attempt multiple mounts
kubectl apply -f test-pod-1.yaml
kubectl apply -f test-pod-2.yaml
kubectl apply -f test-pod-3.yaml

# Expected: All mounts fail with same error
# Expected: No tunnel created
# Expected: No refCount leaks
```

## Impact

### Before Fix
- ❌ First mount with NFS down: Tunnel leak (refCount=1)
- ❌ Subsequent mounts: Fail but tunnel already leaked
- ❌ Inconsistent behavior between first and subsequent mounts
- ❌ Difficult to debug (tunnel appears "healthy" but broken)

### After Fix
- ✅ First mount with NFS down: Clean failure, no leak
- ✅ Subsequent mounts: Consistent behavior
- ✅ Automatic recovery when NFS server comes back
- ✅ Clear error messages indicating NFS server issue
- ✅ No orphaned tunnels or processes

## Related Files

- `pkg/tunnel/manager.go` - Main fix location
- `pkg/tunnel/manager_test.go` - Unit tests
- `docs/TUNNEL_HEALTH_CHECK_FIX.md` - Related health check documentation
- `docs/RFS_STUNNEL_ARCHITECTURE.md` - Architecture overview

## Conclusion

This fix ensures **consistent health checking** across all tunnel operations, preventing tunnel leaks when NFS servers are unreachable during initial mount attempts. The key insight was recognizing the inconsistency between `waitForTunnel()` (used for creation) and `checkTunnelHealth()` (used for ongoing monitoring), and making them use the same health criteria.