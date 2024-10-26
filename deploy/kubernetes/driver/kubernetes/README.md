# CSI Driver deployment developer notes for IBM Cloud Kubernetes service (IKS)

## Namespaces

In general, the following namespaces are used:

| Object Kind | Namespace |
| --- | --- |
| Kustomization | kube-system |
| ConfigMap | kube-system |
| ServiceAccount | default |
| DaemonSet | kube-system |
| Deployment | kube-system |

N.B. Namespaces are only declared in `/manifests`, they are not declared in `/overlays`

## Dependencies

:large_blue_circle:

| Image | DEV | STAGE | STABLE | RELEASE |
| --- | --- | --- | --- | --- |
| registry.k8s.io/sig-storage/csi-node-driver-registrar:v2.9.3 | - | :large_blue_circle: | - | - |
| registry.k8s.io/sig-storage/csi-node-driver-registrar:v2.11.1 | :large_blue_circle: | - | :large_blue_circle: | :large_blue_circle: |
| registry.k8s.io/sig-storage/csi-provisioner:v5.0.2 | :large_blue_circle: | :large_blue_circle: | :large_blue_circle: | :large_blue_circle: |
| registry.k8s.io/sig-storage/csi-resizer:v1.0.0 | - | - | - | - |
| registry.k8s.io/sig-storage/csi-resizer:v1.11.2 | :large_blue_circle: | :large_blue_circle: | :large_blue_circle: | :large_blue_circle: |
| registry.k8s.io/sig-storage/livenessprobe:v2.13.1 | :large_blue_circle: | :large_blue_circle: | :large_blue_circle: | :large_blue_circle: |
| registry.k8s.io/sig-storage/livenessprobe:v2.13.11 | - | - | - | - |
| icr.io/ibm/ibm-vpc-file-csi-driver:v1.0.0 | :large_blue_circle: | - | :large_blue_circle: | - |
| icr.io/ibm/ibm-vpc-file-csi-driver:v1.2.4-beta | - | :large_blue_circle: | - | - |
| icr.io/ibm/ibm-vpc-file-csi-driver:v2.0.4 | - | - | - | - |
| (( concat "{{ DOCKER_REGISTRY }}/armada-master/ibm-vpc-file-csi-driver:" $RELEASE_TAG )) | - | - | - | :large_blue_circle: |
