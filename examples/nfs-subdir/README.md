# Using csi-driver-nfs with IBM VPC File Storage

This guide explains how to use the open-source
[`csi-driver-nfs`](https://github.com/kubernetes-csi/csi-driver-nfs) community driver
to provision multiple Kubernetes PVCs as **subdirectories** on a single IBM VPC File Share.

```
IBM VPC File Share  (one per zone, admin-created)
  └── Mount Target → one VNI → one subnet IP
        ├── PVC-1  →  /<export>/my-pvc-1/       (RWX)
        ├── PVC-2  →  /<export>/my-pvc-2/       (RWX)
        └── PVC-N  →  /<export>/my-pvc-N/       (RWX)
```

Each PVC gets its own isolated subdirectory. All PVCs share **one subnet IP** —
no VPC API calls per PVC, no IP exhaustion.

---

## ⚠️ Important Warnings

> **No Encryption in Transit (EIT).**
> `csi-driver-nfs` performs a plain NFS mount with no integration with IBM VPC EIT
> (DP2 IPSec or RFS Stunnel). **Do not use for regulated or multi-tenant workloads
> that require encryption in transit.**

> **Community driver — not IBM supported.**
> IBM does not provide support for `csi-driver-nfs`.
> Raise issues at: https://github.com/kubernetes-csi/csi-driver-nfs

---

## When to Use

| Scenario | Recommended |
|---|---|
| High PVC count (>500/zone) hitting subnet IP exhaustion | ✅ |
| Dev / test / scratch workloads without EIT requirement | ✅ |
| Multiple RWX consumers sharing one NFS path | ✅ |
| Workloads requiring EIT (DP2 IPSec or RFS Stunnel) | ❌ Use IBM 1:1 mode |
| Regulated / multi-tenant workloads (HIPAA, PCI-DSS, etc.) | ❌ Use IBM 1:1 mode |

> **Why not for regulated / multi-tenant workloads?**
> All PVCs on this pattern share one NFS mount target and one VNI. The only thing
> separating tenant data is Linux filesystem permissions (POSIX `chmod`/`chown`).
> There is no encryption in transit, no VPC-level network isolation between subdirectories,
> and no per-PVC quota enforcement. A privileged container or misconfigured workload could
> traverse into another tenant's subdirectory.
> For regulated or true multi-tenant workloads use IBM VPC File CSI driver in 1:1 mode —
> each PVC gets its own VPC File Share, its own VNI, and optionally EIT (DP2 IPSec or RFS Stunnel).

---

## Prerequisites

- IKS or ROKS cluster with `kubectl` access
- `ibmcloud` CLI with VPC plugin (`ibmcloud plugin install vpc-infrastructure`)
- `helm` v3

---

## Step 1 — Create the VPC File Share

Create one base share per zone. All PVCs in that zone will use subdirectories on this share.

```bash
# Check your target account and resource group first
ibmcloud target

# Create the share — omit --resource-group-name if using the default resource group,
# or add --resource-group-name <name> if you need a specific one
ibmcloud is share-create \
  --name nfs-subdir-base-<clusterID> \
  --zone <zone> \
  --profile dp2 \
  --size 10

# Wait for lifecycle_state: stable
ibmcloud is share <share-id>
```

> **Size guidance:** Each subdirectory draws from the same pool.
> Start with `10 GiB` for testing. Expand later when needed — all subdirectories benefit automatically:
> ```bash
> ibmcloud is share-update <share-id> --size 100
> ```

---

## Step 2 — Discover VPC, Subnet and Security Group

Before creating the mount target you need three values from the cluster.

### 2a — Cluster ID

```bash
# The cluster ID appears in the context name, e.g. mr-conf/daaj53t203s7pnr3u36g/admin
# Extract it:
CLUSTER_ID=$(kubectl config current-context | cut -d'/' -f2)
echo "Cluster ID: $CLUSTER_ID"
```

### 2b — VPC and Subnet

The worker nodes carry their subnet ID as a label. Use any node to look it up:

```bash
# Get subnet ID from a worker node label
SUBNET_ID=$(kubectl get node \
  -o jsonpath='{.items[0].metadata.labels.ibm-cloud\.kubernetes\.io/subnet-id}')
echo "Subnet ID: $SUBNET_ID"

# Resolve subnet → VPC name and zone
ibmcloud is subnet $SUBNET_ID --output json | \
  python3 -c "
import json,sys; d=json.load(sys.stdin)
print('Subnet name:', d['name'])
print('Subnet CIDR:', d['ipv4_cidr_block'])
print('Zone       :', d['zone']['name'])
print('VPC ID     :', d['vpc']['id'])
print('VPC name   :', d['vpc']['name'])
"
```

> **Zone note:** Workers may be in a different zone than the share's zone (e.g. workers in
> `us-south-2`, share in `us-south-1`). Cross-zone NFS within the same VPC works fine.
> Place the VNI subnet in the **same zone as the share** (`us-south-1`) for lowest latency.
> List subnets in the target zone:
>
> ```bash
> # List subnets in the share's zone inside the cluster's VPC
> ibmcloud is subnets --output json | python3 -c "
> import json,sys
> for s in json.load(sys.stdin):
>     if s['vpc']['name'] == '<VPC_NAME>' and s['zone']['name'] == '<SHARE_ZONE>':
>         print(s['id'], s['name'], s['ipv4_cidr_block'])
> "
> ```

### 2c — Security Group

```bash
# List security groups for the cluster — use kube-<clusterID> (not kube-lbaas-* or kube-vpegw-*)
ibmcloud is security-groups --output json | python3 -c "
import json,sys
for sg in json.load(sys.stdin):
    if '<CLUSTER_ID>' in sg['name'] and sg['name'].startswith('kube-') \
       and 'lbaas' not in sg['name'] and 'vpegw' not in sg['name']:
        print('SG ID  :', sg['id'])
        print('SG name:', sg['name'])
"
# Replace <CLUSTER_ID> with your cluster ID from Step 2a
```

---

## Step 3 — Create the Mount Target

Create one mount target per share. Use the cluster's own `kube-<clusterID>` security group
so workers can reach the VNI without any extra SG rules (the existing self-referencing
outbound rule already permits TCP 2049).

```bash
ibmcloud is share-mount-target-create <share-id> \
  --name nfs-subdir-mt-<clusterID> \
  --transit-encryption none \
  --access-protocol nfs4 \
  --vpc <vpc-name-or-id> \
  --subnet <subnet-id-or-name> \
  --vni-name nfs-subdir-vni-<clusterID> \
  --vni-sgs kube-<clusterID> \
  --vni-auto-delete true

# Wait for stable state (lifecycle_state: stable)
ibmcloud is share-mount-target <share-id> <mount-target-id>
# mount_path: <ip>:/<export>
```

Note down:
- **NFS server IP** — e.g. `10.240.0.5`
- **NFS export path** — e.g. `/abc123_def456_...`

---

## Step 4 — Install csi-driver-nfs

`csi-driver-nfs` runs alongside the IBM VPC File CSI driver with no conflict:

| Driver | Provisioner |
|---|---|
| IBM VPC File CSI driver | `vpc.file.csi.ibm.io` |
| csi-driver-nfs | `nfs.csi.k8s.io` |

```bash
helm repo add csi-driver-nfs \
  https://raw.githubusercontent.com/kubernetes-csi/csi-driver-nfs/master/charts
helm repo update

helm install csi-driver-nfs csi-driver-nfs/csi-driver-nfs \
  --namespace kube-system \
  --version v4.13.4 \
  --set controller.hostNetwork=true \
  --set kubeletDir=/var/data/kubelet

# Verify
kubectl get pods -n kube-system -l app.kubernetes.io/instance=csi-driver-nfs
```

> **`--set kubeletDir=/var/data/kubelet` is required on both IKS and ROKS.**
> Both cluster types run kubelet with `--root-dir=/var/data/kubelet`.
> Without this flag the NFS mount bind-propagates to the wrong host path and the
> pod sees `/dev/vda1` instead of the NFS filesystem.
> Use the top-level key `kubeletDir` — **not** `node.kubeletDir`.

---

## Step 5 — Create a StorageClass

> **All StorageClass files** (`01-` through `12-`) contain `<NFS_SERVER_IP>` and
> `<NFS_EXPORT_PATH>` placeholder values. Edit **each file** before applying, replacing
> those two values with the NFS server IP and export path from Step 3.

Edit [`01-storageclass-nfs-subdir.yaml`](./01-storageclass-nfs-subdir.yaml) — replace
`server` and `share` with the values from Step 3:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: vpc-file-nfs-subdir
provisioner: nfs.csi.k8s.io
parameters:
  server: "10.240.0.5"                        # ← NFS server IP from Step 3
  share: "/abc123_def456_..."                 # ← NFS export path from Step 3
  subDir: "${pvc.metadata.name}"              # each PVC gets its own subdirectory
  onDelete: "retain"                          # retain | archive | delete
  mountPermissions: "0777"                    # required for onDelete:delete/archive
reclaimPolicy: Delete
volumeBindingMode: Immediate
allowVolumeExpansion: false
mountOptions:
  - nfsvers=4.1
  - hard
  - sec=sys
```

```bash
kubectl apply -f examples/nfs-subdir/01-storageclass-nfs-subdir.yaml
```

---

## Use Case 1 — Dynamic Subdirectory Provisioning

Apply the StorageClass, PVC, and Deployment from the example files:

```bash
kubectl apply -f examples/nfs-subdir/01-storageclass-nfs-subdir.yaml
kubectl apply -f examples/nfs-subdir/02-pvc-nfs-subdir.yaml
kubectl apply -f examples/nfs-subdir/03-deployment-nfs-subdir.yaml
```

Verify:

```bash
kubectl get pvc my-nfs-subdir-pvc
# STATUS: Bound

POD=$(kubectl get pod -l app=nfs-subdir-app -o name | head -1 | cut -d/ -f2)
kubectl exec $POD -- df -h /volume-mount/data
# Filesystem                                         Size  Used  Avail  Use%  Mounted on
# <NFS_SERVER_IP>:/<export>/my-nfs-subdir-pvc        10G   384K  10G    0%    /volume-mount/data
```

**What happens internally:**
1. Controller mounts the base share temporarily
2. Creates `/<export>/my-nfs-subdir-pvc/` and applies `chmod 0777`
3. Unmounts
4. Returns `volumeHandle = <server>#<export>#my-nfs-subdir-pvc#<uuid>`
5. Node mounts `<server>:/<export>/my-nfs-subdir-pvc` into the pod

---

## Use Case 2 — Multiple PVCs on the Same Share

Each PVC gets an **independent isolated subdirectory** on the same share and subnet IP:

```bash
kubectl apply -f examples/nfs-subdir/02-pvc-nfs-subdir.yaml        # → /<export>/my-nfs-subdir-pvc/
kubectl apply -f examples/nfs-subdir/04-pvc-nfs-subdir-second.yaml  # → /<export>/my-nfs-subdir-pvc-2/
```

```bash
kubectl get pvc
# my-nfs-subdir-pvc    Bound   10Gi  RWX
# my-nfs-subdir-pvc-2  Bound   10Gi  RWX
```

Share layout:
```
/<export>/
  ├── my-nfs-subdir-pvc/        drwxrwxrwx
  └── my-nfs-subdir-pvc-2/      drwxrwxrwx
```

Writes in `my-nfs-subdir-pvc` are invisible to pods mounted on `my-nfs-subdir-pvc-2` — full write isolation.

Verify the subdirectory names differ and isolation holds:

```bash
# Check each PV's subdirectory name
kubectl get pv \
  $(kubectl get pvc my-nfs-subdir-pvc -o jsonpath='{.spec.volumeName}') \
  $(kubectl get pvc my-nfs-subdir-pvc-2 -o jsonpath='{.spec.volumeName}') \
  -o jsonpath='{range .items[*]}{.metadata.name}{" → "}{.spec.csi.volumeAttributes.subDir}{"\n"}{end}'
# pvc-<uuid1> → my-nfs-subdir-pvc
# pvc-<uuid2> → my-nfs-subdir-pvc-2
```

---

## Use Case 3 — Delete Data on PVC Deletion (`onDelete: delete`)

The subdirectory and all its contents are **permanently removed** when the PVC is deleted.
Use only for ephemeral / scratch workloads. **Requires `mountPermissions: "0777"`.**

```bash
kubectl apply -f examples/nfs-subdir/12-storageclass-delete.yaml
kubectl apply -f examples/nfs-subdir/05-pvc-nfs-subdir-delete.yaml

# Write a file to the subdirectory
kubectl run nfs-delete-test --image=nginx:stable-alpine --restart=Never \
  --overrides='{"spec":{"volumes":[{"name":"d","persistentVolumeClaim":{"claimName":"my-delete-pvc"}}],"containers":[{"name":"nfs-delete-test","image":"nginx:stable-alpine","command":["sleep","300"],"volumeMounts":[{"name":"d","mountPath":"/data"}]}]}}'
kubectl wait pod/nfs-delete-test --for=condition=Ready --timeout=60s
kubectl exec nfs-delete-test -- sh -c "echo hello > /data/deleteme.txt && ls /data/"

# Delete the pod, then the PVC
kubectl delete pod nfs-delete-test --force --grace-period=0
kubectl delete pvc my-delete-pvc

# Verify the subdir is gone — csi-driver-nfs ran os.RemoveAll on it
kubectl get pv | grep delete   # should return nothing once fully deleted
```

> **`mountPermissions: "0777"` is required.**
> IBM VPC File shares use `root_squash` — the NFS server maps `uid=0 → nobody (uid=65534)`.
> `os.RemoveAll` only succeeds when the subdirectory is world-writable (`0777`).
> With `0755` it fails with `permission denied` and the PV gets stuck in `Terminating`.

---

## Use Case 4 — Retain Data on PVC Deletion (`onDelete: retain`)

The subdirectory **survives PVC deletion** — data remains on the share for manual recovery.

```bash
kubectl apply -f examples/nfs-subdir/06-storageclass-retain.yaml
kubectl apply -f examples/nfs-subdir/09-pvc-retain.yaml
```

StorageClass key parameters:
```yaml
subDir: "${pvc.metadata.namespace}/${pvc.metadata.name}"  # namespace/pvcname — avoids cross-ns collisions
onDelete: "retain"
mountPermissions: "0755"
reclaimPolicy: Retain
```

After `kubectl delete pvc my-retained-pvc`:

```bash
# PV stays in Released state — data is NOT deleted
kubectl get pv | grep retain
# pvc-<uuid>   10Gi  RWX  Retain  Released  default/my-retained-pvc  vpc-file-nfs-subdir-retain

# Verify subdir and data still exist on the share (via the NFS controller pod)
kubectl exec -n kube-system deploy/csi-nfs-controller -c nfs -- \
  sh -c "mkdir -p /tmp/v && mount -t nfs4 <NFS_SERVER_IP>:<NFS_EXPORT_PATH> /tmp/v && \
         ls /tmp/v/default/my-retained-pvc/ && \
         umount /tmp/v"
# keep-me.txt   ← data intact ✅
```

```
/<export>/default/my-retained-pvc/    ← still present ✅
    └── keep-me.txt                   ← data intact ✅
```

Manual cleanup (when you no longer need the data):
```bash
kubectl exec -n kube-system deploy/csi-nfs-controller -c nfs -- \
  sh -c "mkdir -p /tmp/v && mount -t nfs4 <NFS_SERVER_IP>:<NFS_EXPORT_PATH> /tmp/v && \
         rm -rf /tmp/v/default/my-retained-pvc && umount /tmp/v"

# Then delete the Released PV
kubectl delete pv <pv-name>
```

---

## Use Case 5 — Archive Data on PVC Deletion (`onDelete: archive`)

On PVC deletion the subdirectory is **renamed** to `archived-<pvcname>` — data preserved and browsable.

```bash
kubectl apply -f examples/nfs-subdir/07-storageclass-archive.yaml
kubectl apply -f examples/nfs-subdir/10-pvc-archive.yaml
```

After `kubectl delete pvc my-archived-pvc`:

```bash
# Verify subdir was renamed to archived-<pvcname> on the share
kubectl exec -n kube-system deploy/csi-nfs-controller -c nfs -- \
  sh -c "mkdir -p /tmp/v && mount -t nfs4 <NFS_SERVER_IP>:<NFS_EXPORT_PATH> /tmp/v && \
         ls /tmp/v/ && \
         ls /tmp/v/archived-my-archived-pvc/ && \
         umount /tmp/v"
# archived-my-archived-pvc/   ← renamed ✅
# archive-me.txt               ← data intact ✅
```

```
/<export>/archived-my-archived-pvc/   ← renamed ✅
    └── archive-me.txt                ← data intact ✅
```

> **Requires `mountPermissions: "0777"`** — the controller renames the subdir as `nobody (uid=65534)`.
> With `0755` the rename fails with `permission denied` and the PV gets stuck in `Terminating`.

Manual cleanup:
```bash
kubectl exec -n kube-system deploy/csi-nfs-controller -c nfs -- \
  sh -c "mkdir -p /tmp/v && mount -t nfs4 <NFS_SERVER_IP>:<NFS_EXPORT_PATH> /tmp/v && \
         rm -rf /tmp/v/archived-my-archived-pvc && umount /tmp/v"
```

---

## Use Case 6 — Read-Only Shared Data

Multiple pods share one subdirectory in read-only mode — useful for shared configs,
reference datasets, or static assets.

> ⚠️ **Do NOT add `ro` to `mountOptions` in the StorageClass.**
> The csi-driver-nfs controller mounts the base share during `CreateVolume` to `mkdir`
> the subdirectory. Adding `ro` to the SC's `mountOptions` makes that temporary mount
> read-only too, causing `read-only file system` on `mkdir` → `ProvisioningFailed`.
> Enforce read-only **only at the pod level** using `readOnly: true` in `volumeMount`.

```bash
kubectl apply -f examples/nfs-subdir/08-storageclass-readonly.yaml
```

Key parameters:
```yaml
mountPermissions: "0555"   # r-xr-xr-x — no writes allowed
onDelete: "retain"
# NO "ro" in mountOptions — see warning above
```

Mount in pod (read-only enforcement goes here, not in the StorageClass):
```yaml
volumeMounts:
  - name: data
    mountPath: /config
    readOnly: true    # ← enforces read-only at the pod level
```

---

## Use Case 7 — StatefulSet: Per-Replica Isolated Subdirectories

Each StatefulSet replica gets its own subdirectory via `volumeClaimTemplates` — all sharing
one subnet IP.

```bash
kubectl apply -f examples/nfs-subdir/11-statefulset-nfs-subdir.yaml

# Wait for all replicas
kubectl get pods,pvc -l app=nfs-statefulset -w

# Verify each replica got its own subdir
for i in 0 1 2; do
  PV=$(kubectl get pvc data-nfs-statefulset-$i -o jsonpath='{.spec.volumeName}')
  SUBDIR=$(kubectl get pv $PV -o jsonpath='{.spec.csi.volumeAttributes.subDir}')
  echo "nfs-statefulset-$i → subdir: $SUBDIR"
done
# nfs-statefulset-0 → subdir: data-nfs-statefulset-0
# nfs-statefulset-1 → subdir: data-nfs-statefulset-1
# nfs-statefulset-2 → subdir: data-nfs-statefulset-2

# Verify isolation — each pod sees only its own files
for i in 0 1 2; do
  kubectl exec nfs-statefulset-$i -- sh -c "echo 'data from replica $i' > /volume-mount/data/replica-$i.txt"
done
for i in 0 1 2; do
  echo "nfs-statefulset-$i sees: $(kubectl exec nfs-statefulset-$i -- ls /volume-mount/data/)"
done
# nfs-statefulset-0 sees: replica-0.txt
# nfs-statefulset-1 sees: replica-1.txt
# nfs-statefulset-2 sees: replica-2.txt

# All 3 replicas share ONE subnet IP
kubectl exec nfs-statefulset-0 -- df -h /volume-mount/data
# 10.240.0.x:/<export>/data-nfs-statefulset-0   10G  ...  /volume-mount/data
```

Creates one subdir per replica — all on the same NFS server:
```
/<export>/data-nfs-statefulset-0/
/<export>/data-nfs-statefulset-1/
/<export>/data-nfs-statefulset-2/
```

---

## Use Case 8 — Volume Snapshot and Restore

`csi-driver-nfs` implements CSI snapshots by creating a **`.tar.gz` archive** of the
subdirectory **on the same NFS share**. Restore untars it into a new subdirectory.

> **Requires** the `csi-snapshotter` sidecar and existing VolumeSnapshot CRDs.
> On ROKS the CRDs ship by default. On IKS install them separately.

### Enable snapshots at install time

> **ROKS:** VolumeSnapshot CRDs and the snapshot-controller ship by default
> (`openshift-cluster-storage-operator`). Use `--skip-crds` and disable the bundled
> controller — the OpenShift one is already running.
>
> **IKS:** Install VolumeSnapshot CRDs and snapshot-controller separately first, then
> omit `--skip-crds` and set `externalSnapshotter.controller.enabled=true`.

```bash
# ROKS — upgrade existing install to add snapshotter sidecar
helm upgrade csi-driver-nfs csi-driver-nfs/csi-driver-nfs \
  --namespace kube-system \
  --version v4.13.4 \
  --set controller.hostNetwork=true \
  --set kubeletDir=/var/data/kubelet \
  --set externalSnapshotter.enabled=true \
  --set externalSnapshotter.controller.enabled=false \
  --set externalSnapshotter.customResourceDefinitions.enabled=false \
  --skip-crds

# Verify csi-snapshotter container is present in the controller pod
kubectl get deploy csi-nfs-controller -n kube-system \
  -o jsonpath='{.spec.template.spec.containers[*].name}' | tr ' ' '\n'
# csi-provisioner  csi-resizer  csi-snapshotter  liveness-probe  nfs
```

### Create a VolumeSnapshotClass

```bash
# Edit server/share in 13-volumesnapshotclass.yaml first, then:
kubectl apply -f examples/nfs-subdir/13-volumesnapshotclass.yaml
```

### Take a snapshot

```bash
kubectl apply -f examples/nfs-subdir/14-volumesnapshot.yaml

kubectl wait volumesnapshot/my-nfs-snap-v1 \
  --for=jsonpath='{.status.readyToUse}'=true --timeout=60s
kubectl get volumesnapshot my-nfs-snap-v1
# NAME             READYTOUSE   SOURCEPVC           RESTORESIZE   SNAPSHOTCLASS
# my-nfs-snap-v1   true         my-nfs-subdir-pvc   188           nfs-snapclass
# (ReadyToUse within 2 seconds — it is a tar.gz on NFS, not a VPC block snapshot)
```

### Restore to a new PVC

```bash
kubectl apply -f examples/nfs-subdir/15-pvc-restore-from-snapshot.yaml
kubectl wait pvc/my-nfs-restored-pvc --for=jsonpath='{.status.phase}'=Bound --timeout=60s
kubectl get pvc my-nfs-restored-pvc
# STATUS: Bound
```

Verify restored data:

```bash
kubectl run verify --image=nginx:stable-alpine --restart=Never \
  --overrides='{"spec":{"volumes":[{"name":"d","persistentVolumeClaim":{"claimName":"my-nfs-restored-pvc"}}],"containers":[{"name":"app","image":"nginx:stable-alpine","command":["sleep","60"],"volumeMounts":[{"name":"d","mountPath":"/data"}]}]}}'
kubectl wait pod/verify --for=condition=Ready --timeout=60s
kubectl exec verify -- ls /data/
# v1.txt   ← data from snapshot restored ✅
kubectl delete pod verify --force --grace-period=0
```

**What happens internally:**
- Snapshot: `tar -czvf /<export>/snapshot-<uuid>/pvc-<uuid>.tar.gz /<export>/my-nfs-subdir-pvc/`
- Restore: extracts tar into `/<export>/my-nfs-restored-pvc/`

The snapshot tar is stored on the share at:
```
/<export>/snapshot-<uuid>/pvc-<uuid>.tar.gz
```

> ⚠️ **Snapshot lives on the same share as the source data.**
> If the share is lost, the snapshot is also lost. This is **not an off-share backup**.

---

## Use Case 9 — WaitForFirstConsumer (Topology-Aware Binding)

`WaitForFirstConsumer` keeps the PVC in `Pending` state until a pod that references it
is scheduled onto a node. Only then does the CSI provisioner create the volume.

This is useful when your cluster spans multiple zones and you want the PVC to be
provisioned after the scheduler has decided which node (and zone) the pod will run on.

> **NFS-specific note:** IBM VPC File Shares are available in two availability modes:
>
> - **Zonal** — share is in one zone but the VNI is reachable from **any zone within the same VPC**.
>   A pod scheduled on a node in `us-south-2` can mount a share whose VNI is in `us-south-1` — no problem.
> - **Regional** — share is replicated across zones; mount works from any zone in the region.
>
> Because NFS is not zone-local (unlike block storage), `WaitForFirstConsumer` with `csi-driver-nfs`
> simply defers binding until the pod's node is known — it does **not** constrain or change which
> NFS server is used. Whatever zone the pod lands on, the mount will work.
> Use `WaitForFirstConsumer` only when you have pod affinity, node selector, or taints/tolerations
> that must be resolved by the scheduler before the volume is provisioned.

```bash
kubectl apply -f examples/nfs-subdir/16-storageclass-wffc.yaml
kubectl apply -f examples/nfs-subdir/17-pvc-wffc.yaml

# PVC stays Pending until a pod references it — confirm:
kubectl get pvc my-wffc-pvc
# NAME          STATUS    VOLUME   ...
# my-wffc-pvc   Pending            ← no pod yet, binding deferred ✅

# Now deploy the app
kubectl apply -f examples/nfs-subdir/18-deployment-wffc.yaml

# PVC binds as soon as pod is scheduled onto a node
kubectl wait pvc/my-wffc-pvc --for=jsonpath='{.status.phase}'=Bound --timeout=120s
kubectl get pvc my-wffc-pvc
# NAME          STATUS   VOLUME             CAPACITY   ...
# my-wffc-pvc   Bound    pvc-<uuid>         10Gi       ← bound after pod scheduled ✅

# Check which node was selected
kubectl get pvc my-wffc-pvc \
  -o jsonpath='{.metadata.annotations.volume\.kubernetes\.io/selected-node}'
# test-<clusterID>-default-<nodeID>
```

```bash
# Verify subdir was created on the share
kubectl exec -n kube-system deploy/csi-nfs-controller -c nfs -- \
  sh -c "mkdir -p /tmp/v && \
         mount -t nfs4 <NFS_SERVER_IP>:<NFS_EXPORT_PATH> /tmp/v && \
         ls /tmp/v/ && umount /tmp/v"
# my-wffc-pvc/   ← subdir created after pod triggered provisioning ✅
```

| Mode | PVC binds | Use when |
|---|---|---|
| `Immediate` (default) | As soon as PVC is created | No topology constraints, simple setup |
| `WaitForFirstConsumer` | Only after pod is scheduled | Pod has node/zone affinity, avoid pre-binding to wrong zone |

---

## `subDir` Template Variables

| Variable | Resolves to | Example |
|---|---|---|
| `${pvc.metadata.name}` | PVC name | `my-pvc` |
| `${pvc.metadata.namespace}` | PVC namespace | `default` |
| `${pv.metadata.name}` | PV name (UUID-based) | `pvc-3b9b2a95-...` |

```yaml
subDir: "${pvc.metadata.name}"                            # my-pvc
subDir: "${pvc.metadata.namespace}/${pvc.metadata.name}"  # default/my-pvc
subDir: "team-a/${pvc.metadata.name}"                     # team-a/my-pvc
subDir: "${pv.metadata.name}"                             # pvc-3b9b2a95-...
```

---

## `onDelete` Policy Reference

| Value | On PVC Delete | Notes |
|---|---|---|
| `retain` | Subdir left in place | Data survives; manual cleanup needed |
| `archive` | Subdir renamed `archived-<pvcname>` | Data preserved; requires `mountPermissions: "0777"` |
| `delete` | `os.RemoveAll` on subdir | Data deleted; requires `mountPermissions: "0777"` |

**`mountPermissions` and `onDelete` interaction on IBM VPC File shares:**

IBM VPC File shares use `root_squash` — the NFS server maps `uid=0 → nobody (uid=65534)`.
The controller creates each subdir as `nobody`. Whether `delete`/`archive` succeeds depends
on the subdir's mode, not on IKS vs ROKS:

| `mountPermissions` | `onDelete: delete` | `onDelete: archive` |
|---|---|---|
| `"0777"` | ✅ Works (confirmed IKS + ROKS 4.22) | ✅ Works |
| `"0755"` | ❌ `permission denied` | ❌ `permission denied` |

**Always use `mountPermissions: "0777"` when using `onDelete: delete` or `archive`.**

---

## Cleanup (correct order)

> ⚠️ Always delete PVCs **before** uninstalling the Helm release.
> If you uninstall first, `Delete`-policy PVs get stuck in `Terminating`.

```bash
# 1. Delete workloads
kubectl delete deployment nfs-subdir-app --ignore-not-found

# 2. Delete PVCs — let controller process DeleteVolume
kubectl delete pvc --selector=app=nfs-subdir --ignore-not-found
kubectl get pv | grep nfs   # wait until empty

# 3. Delete StorageClasses
kubectl delete sc vpc-file-nfs-subdir vpc-file-nfs-subdir-retain \
  vpc-file-nfs-subdir-archive vpc-file-nfs-subdir-readonly --ignore-not-found

# 4. Uninstall driver
helm uninstall csi-driver-nfs -n kube-system
```

### Recovery — PV stuck in Terminating

```bash
# Force-remove the finalizer
kubectl patch pv <pv-name> -p '{"metadata":{"finalizers":[]}}' --type=merge
```

> The NFS subdir will **not** be cleaned up — manually remove it via NFS mount if needed.

---

## Limitations

| Limitation | Detail |
|---|---|
| **No EIT** | Plain NFS only — no DP2 IPSec, no RFS Stunnel |
| **No per-PVC quota** | `storage:` in PVC spec is metadata — NFS enforces nothing |
| **No per-PVC volume expansion** | Expand the base share; all subdirs benefit automatically |
| **Snapshot stored on same share** | If the share is lost, snapshots are lost too — not an off-share backup |
| **Snapshot not crash-consistent** | `tar` runs while data may be live — no quiescing |
| **`onDelete: delete/archive` requires `0777`** | IBM VPC File `root_squash` maps uid=0→nobody; needs world-write on subdir |
| **Single point of failure** | All PVCs depend on one share and one mount target |
| **Shared IOPS** | All PVCs share the base share's IOPS budget |
| **No IBM support** | Community driver — raise issues at github.com/kubernetes-csi/csi-driver-nfs |

---

## Coexistence with IBM VPC File CSI Driver

Both drivers run simultaneously in `kube-system` with no conflict:

| Driver | Provisioner | StorageClass example |
|---|---|---|
| IBM VPC File CSI driver | `vpc.file.csi.ibm.io` | `ibmc-vpc-file-dp2` |
| csi-driver-nfs | `nfs.csi.k8s.io` | `vpc-file-nfs-subdir` |

Existing IBM VPC File PVCs and StorageClasses are **not affected**.
