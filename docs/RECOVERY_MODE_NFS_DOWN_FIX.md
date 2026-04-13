# Critical Fix: Recovery Mode with NFS Server Down

## Problem Statement

**Critical Issue:** When tunnel-manager restarts while NFS server is temporarily unreachable, it fails to recreate tunnels for existing pod mounts, breaking active workloads.

### The Scenario

```
T+0s  Pod-1 running, using tunnel on port 30000
T+0s  NFS server goes DOWN (network issue, maintenance, etc.)
T+0s  Tunnel-manager crashes/restarts
T+1s  RecoverFromCrash() tries to recreate tunnel
T+1s  waitForTunnel() checks NFS server connectivity
T+1s  NFS server unreachable → tunnel creation FAILS
T+1s  ❌ NO TUNNEL CREATED
T+1s  ❌ Pod-1's mount is now BROKEN (no tunnel!)
T+1s  ❌ Pod-1 can't access files anymore
```

**Impact:** Existing pods with active mounts lose access to their volumes when tunnel-manager restarts, even though the mounts still exist in the kernel.

## Root Cause

The issue was caused by **applying the same health check logic to both new tunnel creation and tunnel recovery**:

### Before Fix

```go
func (m *Manager) waitForTunnel(t *Tunnel, timeout time.Duration) error {
    for time.Now().Before(deadline) {
        if localPortListening {
            // ALWAYS check NFS server connectivity
            if m.checkNFSServerConnectivity(t) {
                return nil  // ✅ Success
            }
            // NFS down → keep retrying
        }
    }
    return fmt.Errorf("timeout")  // ❌ Fail after 10s
}
```

**Problem:** This logic is correct for **new tunnel creation** but wrong for **tunnel recovery**:

| Scenario | NFS Check Needed? | Why? |
|----------|-------------------|------|
| **New tunnel** (first mount) | ✅ YES | Prevent creating broken tunnels with refCount leaks |
| **Recovery** (restart) | ❌ NO | Pod's mount already exists, just need tunnel process |

### The Distinction

**New Tunnel Creation:**
- Pod requesting mount for first time
- No existing mount in kernel
- If NFS server down, mount will fail anyway
- **Must verify NFS connectivity** to prevent tunnel leak

**Tunnel Recovery:**
- Pod already has active mount in kernel
- Mount was working before tunnel-manager restart
- NFS server may be temporarily unreachable
- **Must restore tunnel** to resume service when NFS recovers

## The Solution

### Added `isRecovery` Parameter

Modified `waitForTunnel()` to distinguish between new creation and recovery:

```go
func (m *Manager) waitForTunnel(t *Tunnel, timeout time.Duration, isRecovery bool) error {
    for time.Now().Before(deadline) {
        if localPortListening {
            if !isRecovery {
                // NEW TUNNEL: Verify NFS server reachable
                if m.checkNFSServerConnectivity(t) {
                    return nil  // ✅ Success only if NFS reachable
                }
                // NFS down → keep retrying
            } else {
                // RECOVERY: Tunnel process ready is sufficient
                // Pod's mount already exists, just need tunnel running
                return nil  // ✅ Success if process ready
            }
        }
    }
    return fmt.Errorf("timeout")
}
```

### Call Sites Updated

1. **New Tunnel Creation** (`EnsureTunnel` → `createTunnel`):
   ```go
   return m.createTunnel(volumeID, nfsServer, port, 1, true, false)
   //                                                      ^^^^^ isRecovery=false
   ```

2. **Tunnel Recovery** (`RecoverFromCrash` → `recoverTunnel` → `createTunnel`):
   ```go
   return m.createTunnel(volumeID, nfsServer, port, refCount, false, true)
   //                                                                 ^^^^ isRecovery=true
   ```

3. **Tunnel Restart** (`restartTunnel`):
   ```go
   if err := m.waitForTunnel(t, 10*time.Second, true); err != nil {
   //                                            ^^^^ isRecovery=true
   ```

## Behavior After Fix

### Scenario 1: New Mount with NFS Server Down

```
T+0s  Pod-1 requests mount for volume-A (first time)
T+0s  NFS server is DOWN
T+0s  EnsureTunnel() called
T+0s  createTunnel() with isRecovery=false
T+0s  Stunnel process starts, local port listening
T+0s  waitForTunnel() checks NFS server: ❌ UNREACHABLE
T+1s  waitForTunnel() retries...
T+2s  waitForTunnel() retries...
...
T+10s waitForTunnel() timeout
T+10s createTunnel() fails, cleanup:
      - Stops stunnel process
      - Releases port
      - Removes config file
T+10s ✅ NO TUNNEL LEAK
T+10s Mount fails with clear error message

Result: ✅ Correct - prevents tunnel leak on first mount
```

### Scenario 2: Recovery with NFS Server Down (FIXED!)

```
T+0s  Pod-1 running with volume-A mounted
T+0s  Tunnel exists: refCount=1, port=30000
T+0s  NFS server goes DOWN
T+0s  Tunnel-manager crashes/restarts
T+1s  RecoverFromCrash() called
T+1s  Loads metadata: volume-A, port=30000, refCount=1
T+1s  Verifies /proc/mounts: actualMountCount=1 ✅
T+1s  recoverTunnel() with isRecovery=true
T+1s  Stunnel process starts on port 30000
T+1s  waitForTunnel() checks local port: ✅ LISTENING
T+1s  waitForTunnel() skips NFS check (isRecovery=true)
T+1s  ✅ TUNNEL RECOVERED: refCount=1, port=30000
T+1s  Metadata saved
T+2s  Tunnel-manager fully operational
T+2s  Pod-1 mount still exists, tunnel restored
T+5s  NFS server comes back online
T+5s  ✅ Pod-1 can access files again (automatic recovery)

Result: ✅ Correct - tunnel restored, service resumes when NFS recovers
```

### Scenario 3: Recovery with NFS Server Healthy

```
T+0s  Pod-1 running with volume-A mounted
T+0s  Tunnel exists: refCount=1, port=30000
T+0s  NFS server is HEALTHY
T+0s  Tunnel-manager crashes/restarts
T+1s  RecoverFromCrash() called
T+1s  Loads metadata: volume-A, port=30000, refCount=1
T+1s  Verifies /proc/mounts: actualMountCount=1 ✅
T+1s  recoverTunnel() with isRecovery=true
T+1s  Stunnel process starts on port 30000
T+1s  waitForTunnel() checks local port: ✅ LISTENING
T+1s  waitForTunnel() skips NFS check (isRecovery=true)
T+1s  ✅ TUNNEL RECOVERED: refCount=1, port=30000
T+1s  Pod-1 continues working without interruption

Result: ✅ Correct - fast recovery, no service disruption
```

### Scenario 4: Tunnel Restart (Health Check Failure)

```
T+0s  Pod-1 running with volume-A mounted
T+0s  Tunnel process crashes unexpectedly
T+0s  monitorProcess() detects crash
T+0s  restartTunnel() called
T+0s  Starts new stunnel process
T+0s  waitForTunnel() with isRecovery=true
T+0s  Checks local port: ✅ LISTENING
T+0s  Skips NFS check (isRecovery=true)
T+0s  ✅ TUNNEL RESTARTED
T+0s  Pod-1 continues working

Result: ✅ Correct - fast restart, no NFS check needed
```

## Key Differences

### New Tunnel Creation (isRecovery=false)

**Purpose:** Prevent tunnel leaks when NFS server is down

**Checks:**
1. ✅ Stunnel process running
2. ✅ Local port listening
3. ✅ **NFS server reachable** ← Critical for new tunnels

**Behavior:**
- Fails if NFS server unreachable
- No tunnel created, no refCount leak
- Clear error message to user

**Use Cases:**
- First pod mounting a volume
- New volume mount request

### Tunnel Recovery (isRecovery=true)

**Purpose:** Restore service for existing mounts

**Checks:**
1. ✅ Stunnel process running
2. ✅ Local port listening
3. ⏭️ **NFS server check skipped** ← Critical for recovery

**Behavior:**
- Succeeds if tunnel process ready
- Restores tunnel even if NFS temporarily down
- Service resumes when NFS recovers

**Use Cases:**
- Tunnel-manager restart/crash
- Node reboot recovery
- Tunnel process restart after crash

## Benefits

### 1. Service Continuity
- ✅ Existing pods maintain access after tunnel-manager restart
- ✅ No service disruption during NFS temporary outages
- ✅ Automatic recovery when NFS server comes back

### 2. Correct Behavior for New Mounts
- ✅ Still prevents tunnel leaks on first mount
- ✅ Clear error messages when NFS server down
- ✅ No stale tunnels with incorrect refCounts

### 3. Operational Resilience
- ✅ Tunnel-manager can restart safely during NFS issues
- ✅ No manual intervention needed
- ✅ Graceful degradation and recovery

## Testing

### Test 1: Recovery with NFS Down
```bash
# Start with pod running
kubectl apply -f test-pod.yaml
kubectl wait --for=condition=Ready pod/test-pod

# Verify mount working
kubectl exec test-pod -- ls /mnt/volume

# Simulate NFS server down
iptables -A OUTPUT -p tcp --dport 2049 -d 10.240.0.5 -j DROP

# Restart tunnel-manager
kubectl delete pod -n kube-system -l app=tunnel-manager

# Expected: Tunnel recovered (check logs)
# Expected: Pod still running (mount exists)
# Expected: File access fails (NFS down)

# Restore NFS connectivity
iptables -D OUTPUT -p tcp --dport 2049 -d 10.240.0.5 -j DROP

# Expected: File access works again (automatic recovery)
kubectl exec test-pod -- ls /mnt/volume
```

### Test 2: New Mount with NFS Down
```bash
# Simulate NFS server down
iptables -A OUTPUT -p tcp --dport 2049 -d 10.240.0.5 -j DROP

# Try to create new pod
kubectl apply -f test-pod.yaml

# Expected: Pod stuck in ContainerCreating
# Expected: Clear error about NFS server unreachable
# Expected: No tunnel leak (check tunnel-manager logs)

# Restore NFS connectivity
iptables -D OUTPUT -p tcp --dport 2049 -d 10.240.0.5 -j DROP

# Expected: Pod becomes Running automatically
```

## Related Files

- `pkg/tunnel/manager.go` - Main implementation
- `docs/FIRST_MOUNT_NFS_DOWN_FIX.md` - Related fix for first mount
- `docs/POD_RESTART_COMPLETE_SOLUTION.md` - Complete restart scenarios

## Conclusion

This fix ensures tunnel-manager can safely restart and recover tunnels even when NFS servers are temporarily unreachable, maintaining service continuity for existing workloads while still preventing tunnel leaks for new mounts.

The key insight is **distinguishing between new tunnel creation (must verify NFS) and tunnel recovery (restore service first, NFS check not needed)**.