# Tunnel Manager Communication: gRPC vs HTTP

## Current Implementation: HTTP over Unix Socket

The tunnel-manager currently uses **HTTP/JSON over Unix domain socket** for communication between the CSI node server and tunnel-manager sidecar.

## Important Context: CSI Driver Already Uses gRPC

The IBM VPC File CSI Driver **already has gRPC infrastructure** in place:

```go
// From go.mod
google.golang.org/grpc v1.65.0
google.golang.org/protobuf v1.36.1
github.com/container-storage-interface/spec v1.11.0

// From pkg/ibmcsidriver/server.go
import "google.golang.org/grpc"
server := grpc.NewServer(opts...)
```

**The CSI spec itself uses gRPC** for Controller and Node services, so the build infrastructure, dependencies, and team familiarity are already in place.

### Current Architecture

```
CSI Node Server Container          Tunnel Manager Container
┌─────────────────────┐            ┌──────────────────────┐
│                     │            │                      │
│  HTTP Client        │───────────▶│  HTTP Server         │
│  (http_service.go)  │  Unix      │  (http_service.go)   │
│                     │  Socket    │                      │
│  - EnsureTunnel()   │            │  - POST /v1/tunnels  │
│  - RemoveTunnel()   │            │  - GET /v1/tunnels   │
│  - GetTunnel()      │            │  - DELETE /v1/tunnels│
│                     │            │                      │
└─────────────────────┘            └──────────────────────┘
         │                                    │
         └────────────────────────────────────┘
              /var/lib/kubelet/plugins/vpc.file.csi.ibm.io/tunnel-manager.sock
```

## Comparison: gRPC vs HTTP

### HTTP over Unix Socket (Current)

**Advantages:**
- ✅ **Simple implementation** - Standard library, no protobuf
- ✅ **Easy debugging** - Can use `curl` or `nc` to test
- ✅ **Human-readable** - JSON payloads easy to inspect
- ✅ **Lightweight** - No code generation needed
- ✅ **Flexible** - Easy to add new endpoints
- ✅ **No dependencies** - Uses only Go standard library
- ✅ **Works well for simple APIs** - 3-4 operations total

**Disadvantages:**
- ⚠️ Manual JSON marshaling/unmarshaling
- ⚠️ No type safety at compile time
- ⚠️ No built-in streaming support
- ⚠️ Manual error handling

### gRPC over Unix Socket

**Advantages:**
- ✅ **Type safety** - Protobuf definitions enforce contracts
- ✅ **Code generation** - Client/server stubs auto-generated
- ✅ **Streaming support** - Built-in bidirectional streaming
- ✅ **Better performance** - Binary protocol, HTTP/2
- ✅ **Versioning** - Protobuf handles backward compatibility
- ✅ **Standard CSI pattern** - CSI spec uses gRPC

**Disadvantages:**
- ❌ **More complex** - Requires protobuf definitions
- ❌ **Code generation** - Need protoc compiler in build
- ❌ **Harder to debug** - Binary protocol, need grpcurl
- ❌ **More dependencies** - google.golang.org/grpc
- ❌ **Overkill for simple APIs** - 3-4 operations don't need gRPC
- ❌ **Build complexity** - Need to manage .proto files

## Performance Comparison

### HTTP/JSON over Unix Socket
```
Request:  ~100-200 bytes (JSON)
Response: ~100-200 bytes (JSON)
Latency:  ~1-2ms (local Unix socket)
Overhead: JSON parsing ~50-100μs
```

### gRPC over Unix Socket
```
Request:  ~50-100 bytes (protobuf)
Response: ~50-100 bytes (protobuf)
Latency:  ~0.5-1ms (local Unix socket, HTTP/2)
Overhead: Protobuf parsing ~10-20μs
```

**Verdict:** For local Unix socket communication, the performance difference is **negligible** (~1ms vs ~0.5ms). The bottleneck is tunnel creation (100-500ms), not IPC.

## Recommendation

### Keep HTTP/JSON (Current Implementation) ✅

**Reasons:**

1. **Simplicity Wins**
   - Only 4 operations: EnsureTunnel, RemoveTunnel, GetTunnel, Health
   - No streaming needed
   - No complex data structures

2. **Operational Benefits**
   - Easy to debug with curl/nc
   - Human-readable logs
   - No build-time code generation

3. **Performance is Sufficient**
   - Unix socket is already very fast
   - Tunnel creation (100-500ms) dominates latency
   - JSON overhead (~50μs) is negligible

4. **Maintenance**
   - Fewer dependencies
   - Simpler build process
   - Easier for contributors to understand

### When to Consider gRPC

Consider switching to gRPC if:
- ❌ Need streaming (e.g., tunnel health events)
- ❌ API grows to 10+ operations
- ❌ Need strong versioning guarantees
- ❌ Performance becomes critical (>1000 ops/sec)
- ❌ Want to match CSI spec pattern exactly

**Current usage:** ~10-50 ops/sec per node → HTTP is fine

## Implementation Comparison

### Current HTTP Implementation

```go
// Client (CSI Node Server)
client := tunnel.NewHTTPClient(socketPath, logger)
info, err := client.EnsureTunnel(ctx, volumeID, nfsServer)

// Server (Tunnel Manager)
http.HandleFunc("/v1/tunnels/ensure", handleEnsureTunnel)
```

**Lines of code:** ~300 lines total

### Hypothetical gRPC Implementation

```protobuf
// tunnel.proto
service TunnelManager {
  rpc EnsureTunnel(EnsureTunnelRequest) returns (TunnelInfo);
  rpc RemoveTunnel(RemoveTunnelRequest) returns (Empty);
  rpc GetTunnel(GetTunnelRequest) returns (TunnelInfo);
  rpc Health(Empty) returns (HealthResponse);
}
```

```go
// Client (CSI Node Server)
conn, _ := grpc.Dial("unix://"+socketPath)
client := pb.NewTunnelManagerClient(conn)
info, err := client.EnsureTunnel(ctx, &pb.EnsureTunnelRequest{...})

// Server (Tunnel Manager)
type server struct {
    pb.UnimplementedTunnelManagerServer
    manager *Manager
}
func (s *server) EnsureTunnel(ctx, req) (*pb.TunnelInfo, error) {...}
```

**Lines of code:** ~500 lines + protobuf definitions + build setup

## Debugging Comparison

### HTTP (Current)

```bash
# Test tunnel creation
curl --unix-socket /var/lib/kubelet/plugins/vpc.file.csi.ibm.io/tunnel-manager.sock \
  -X POST http://unix/v1/tunnels/ensure \
  -d '{"volumeID":"vol-123","nfsServer":"10.0.0.1"}'

# Response (human-readable)
{
  "volumeID": "vol-123",
  "remoteAddr": "10.0.0.1",
  "localPort": 20042,
  "state": "running",
  "refCount": 1
}
```

### gRPC (Hypothetical)

```bash
# Need grpcurl installed
grpcurl -unix /var/lib/kubelet/plugins/vpc.file.csi.ibm.io/tunnel-manager.sock \
  -d '{"volumeID":"vol-123","nfsServer":"10.0.0.1"}' \
  tunnel.TunnelManager/EnsureTunnel

# Response (still readable but requires grpcurl)
{
  "volumeID": "vol-123",
  "remoteAddr": "10.0.0.1",
  "localPort": 20042,
  "state": "running",
  "refCount": 1
}
```

## Conclusion

**REVISED Recommendation: Consider Migrating to gRPC**

Given that the CSI driver **already has gRPC infrastructure**, the decision changes:

### Arguments FOR gRPC (Stronger Now)

1. ✅ **Infrastructure Already Exists**
   - gRPC dependencies already in go.mod
   - Build system already handles protobuf
   - Team already familiar with gRPC patterns
   - No additional complexity to the project

2. ✅ **Consistency with CSI Spec**
   - CSI Controller/Node services use gRPC
   - Tunnel manager would follow same pattern
   - Unified debugging and monitoring approach

3. ✅ **Type Safety**
   - Compile-time contract enforcement
   - Prevents API mismatches between client/server
   - Better IDE support and autocomplete

4. ✅ **Future-Proof**
   - Easy to add streaming (tunnel health events)
   - Better versioning support
   - Standard pattern for service-to-service communication

### Arguments FOR HTTP (Still Valid)

1. ✅ **Simpler Debugging**
   - Can use curl for testing
   - Human-readable JSON logs
   - No need for grpcurl

2. ✅ **Already Implemented**
   - Working code in production
   - Migration effort required
   - Risk of introducing bugs

### Updated Recommendation

**Short-term:** Keep HTTP/JSON (it works, don't break it)

**Medium-term:** Migrate to gRPC when:
- Adding new features (streaming, metrics)
- Major refactoring needed
- Team has bandwidth for migration

**Benefits of Migration:**
- Consistency with CSI patterns
- Better type safety
- No additional dependencies (already have gRPC)
- Future-proof for new features

**Migration Effort:** ~2-3 days
- Create protobuf definitions (~1 day)
- Implement gRPC server/client (~1 day)
- Testing and validation (~1 day)

### Conclusion

Since gRPC infrastructure already exists, **the cost of using gRPC is much lower** than initially assessed. However, **HTTP/JSON works fine** for the current use case.

**Recommendation:** Keep HTTP for now, but **plan migration to gRPC** in next major version or when adding new features.