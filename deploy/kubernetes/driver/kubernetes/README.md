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
