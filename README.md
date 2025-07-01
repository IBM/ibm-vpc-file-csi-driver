# ibm-vpc-file-csi-driver

[![Build Status](https://api.travis-ci.com/IBM/ibm-vpc-file-csi-driver.svg?branch=master)](https://travis-ci.com/IBM/ibm-vpc-file-csi-driver)
[![Coverage](https://github.com/IBM/ibm-vpc-file-csi-driver/blob/gh-pages/coverage/master/badge.svg)](https://github.com/IBM/ibm-vpc-file-csi-driver/tree/gh-pages/coverage/master/cover.html)

The `ibm-vpc-file-csi-driver` is a [CSI plugin](https://kubernetes-csi.github.io/docs/developing.html) for managing the life cycle of [IBM Cloud File Storage For VPC](https://cloud.ibm.com/docs/vpc?topic=vpc-file-storage-vpc-about).

The driver consists mainly of,
- **vpc-file-csi-controller** controller deployment pods.
- **vpc-file-csi-node** node server daemonset pods.

**Note:** 
- The _code maintained_ in this repository is for both **Self-managed clusters**: Kubernetes or RedHat OpenShift Container Platform (OCP) clusters and **IBM Cloud Managed services**: IBM Kubernetes Service (IKS) and RedHat OpenShift Kubernetes Service (ROKS).
- The _manifests provided_ though here applies for **self-managed** Kubernetes or RedHat OpenShift Container Platform (OCP) clusters ONLY, but the driver is **NOT TESTED for self-managed clusters**.
- The steps shared below "should" work for **self-managed** Kubernetes or RedHat OpenShift Container Platform (OCP) clusters but in case of any issues please open an issue in this repo. Refer to the [Self-Managed Prerequisites](#self-managed-prerequisites) section below for more details.

## Supported features

| Feature | Description | Supported |
|---------|-------------|-----------|
| Static Provisioning   | Associate an externally-created IBM FileShare volume with a [PersistentVolume](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) (PV) and use it with your application.| ✅ |
| Dynamic Provisioning  | Automatically create IBM FileShare volumes and associated [PersistentVolumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) (PV) from [PersistentVolumeClaims](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#dynamic) (PVC). Parameters can be passed via a [StorageClass](https://kubernetes.io/docs/concepts/storage/storage-classes/#the-storageclass-resource) for fine-grained control over volume creation. | ✅ |
| Volume Resizing       | Expand the volume by specifying a new size in the [PersistentVolumeClaim](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#expanding-persistent-volumes-claims) (PVC).| ✅ |
| Volume Snapshots      | Create and restore volume snapshots.| ❌ |
| Volume Cloning        | Create a new volume from an existing volume.| ❌ |


For more imformation visit the public documentation of file addon for IKS and ROKS at [IBM Cloud File Storage Share CSI Driver](https://cloud.ibm.com/docs/containers?topic=containers-storage-file-vpc-apps).

## Supported platforms

This CSI Driver can be used on all supported versions of **IBM Cloud Managed services**: IBM Kubernetes Service (IKS) and RedHat OpenShift Kubernetes Service (ROKS). To get the versions supported, please refer to

- [Kubernetes Version Information](https://cloud.ibm.com/docs/containers?topic=containers-cs_versions#cs_versions_available)
- [RedHat Openshift on IBM Cloud Version Information](https://cloud.ibm.com/docs/openshift?topic=openshift-openshift_versions#os-openshift-with-coreos)

## Utilities Needed

Make sure to have these tools installed in your system:
- [Go](https://golang.org/doc/install) (Any supported version)
- make (GNU Make) (version 3.8 or later)
- [Docker](https://docs.docker.com/get-docker/) (version 20.10.24 or later)
- [Kustomize](https://kubernetes-sigs.github.io/kustomize/installation/) (version 5.0.1 or later)
- [Kubectl](https://kubernetes.io/docs/tasks/tools/) (Any supported version)
- [IBM Cloud CLI](https://cloud.ibm.com/docs/cli?topic=cli-install-ibmcloud-cli) (Any supported version)

## How to contribute

If you have any questions or issues you can create a new [GitHub Issue](https://github.com/IBM/ibm-vpc-file-csi-driver/issues/new) in this repository.

Pull requests are very welcome! Make sure your patches are well tested. Ideally create a topic branch for every separate change you make. For example:

1. Fork the repo
2. Create your feature branch (git checkout -b my-new-feature)
3. Commit your changes (git commit -am 'Added some feature')
4. Push to the branch (git push origin my-new-feature)
5. Create new Pull Request
6. Add the test results in the PR

```shell
mkdir -p $GOPATH/src/github.com/IBM
cd $GOPATH/src/github.com/IBM
# Fork the repository, use your fork URL instead of the original repo URL
git clone https://github.com/myusername/ibm-vpc-file-csi-driver.git
cd ibm-vpc-file-csi-driver
```

## Makefile Targets

The Makefile provides several targets to help with development, testing, and building the driver. Here are some of the key targets:

- `make test`         - Run all tests
- `make test-sanity`  - Run sanity tests
- `make coverage`     - Generate code coverage report
- `make build`        - Build the CSI Driver binary
- `make buildimage`   - Build the CSI Driver container image

## Image To be used
User can either use the image which is created for _addon_ for "managed" IBMCloud clusters, or can build the image manually.
- To use the "_addon_" image, refer to [file version change log](https://cloud.ibm.com/docs/containers?topic=containers-cl-add-ons-vpc-file-csi-driver) and get the image tag. For eg, you will find version `2.0.10_334`, so the image tag will be `v2.0.10`.
- To build the image manually run command `make buildimage` in the root of the repository. This will create a container image with tag `latest-<CPU_ARCH>`, where `<CPU_ARCH>` is the CPU architecture of the host machine, such as `amd64` or `arm64`. Note: The image will be created under the name `ibm-vpc-file-csi-driver`.

#### Push container image to a container registry

The container image should be pushed to any container registry that the cluster worker nodes have access/authorization to pull images from; these can be private or public. You may use [docker.io](https://hub.docker.com/) or [IBM Cloud Container Registry](https://cloud.ibm.com/docs/Registry). 

- For using IBM private registry, refer to [IBM Cloud Container Registry documentation](https://cloud.ibm.com/docs/Registry?topic=Registry-getting-started).
- In order to use private registry, you need to create an image pull secret in the cluster. The image pull secret is used by the cluster to authenticate and pull the container image from the registry.

  - `--docker-username`: `iamapikey`.
  - `--docker-email`: `iamapikey`.
  - `--docker-server`: Enter the registry URL, such as `icr.io` for IBM Cloud Container Registry. If using regional registry, use the URL such as `us.icr.io`, `eu.icr.io`, or `jp.icr.io`.
  - `--docker-password`: Enter your IAM API key. For more information about IAM API keys, see https://cloud.ibm.com/docs/account?topic=account-manapikey
  - `--namespace`: Enter the namespace where the manifests are applied.

  ```
  kubectl create secret docker-registry icr-io-secret --docker-username=iamapikey --docker-email=iamapikey --docker-server=<registry-url> --docker-password=-<iam-api-key> -n <namespace>
  ```

## Self-Managed Prerequisites
Following are the prerequisites to use the IBM Cloud File Storage Share CSI Driver:

1. User should have either Red Hat® OpenShift® or Kubernetes cluster running on IBM Cloud Infrastructure, with VPC networking.
  - This CSI Driver does not apply to cluster resources using IBM Cloud (Classic) with VLANs, aka. IBM Cloud Classic Infrastructure.
2. Access to IBM Cloud to identify the required worker/node details. Either using `ibmcloud` CLI with the Infrastructure services CLI Plugin (`ibmcloud plugin install is`), or [`IBM Cloud Console`](https://cloud.ibm.com) Web GUI.
3. The VPC Security Group applied to the cluster worker node's (e.g. `-cluster-wide`) must allow TCP 2049 for the NFS protocol.
4. The cluster's worker node should have following labels for Region and Availability Zone, if not please apply labels to all target nodes before deploying the IBM Cloud File Storage Share CSI Driver.

   ```yaml
   "failure-domain.beta.kubernetes.io/region"
   "failure-domain.beta.kubernetes.io/zone"
   "topology.kubernetes.io/region"
   "topology.kubernetes.io/zone"
   "ibm-cloud.kubernetes.io/vpc-instance-id"
   "ibm-cloud.kubernetes.io/worker-id" # Required for IKS, can remain blank for OCP Self-Managed
   ```
   4.1 Please use the `apply-required-setup.sh` script for all the nodes in the cluster. The script requires the following inputs:

   - **instanceID**: Obtain this from `ibmcloud is ins`
   - **node-name**: The node name as shown in `kubectl get nodes`
   - **region-of-instanceID**: The region of the instanceID, get this from `ibmcloud is in <instanceID>`
   - **zone-of-instanceID**: The zone of the instanceID, get this from `ibmcloud is in <instanceID>`

   Example usage:
   ```shell
   ./scripts/apply-required-setup.sh <instanceID> <node-name> <region-of-instanceID> <zone-of-instanceID>
   ```

   **Note:** The `apply-required-setup.sh` script is idempotent, safe to run multiple times.
   
5. The cluster should have the `ibm-cloud-provider-data` configmap created in the same namespace as your manifests are applied. This configmap contains the "VPC ID" and "VPC Subnet IDs" required for the CSI Driver to function properly.
6. The cluster should have the `ibm-cloud-cluster-info` configmap created in the same namespace as your manifests are applied. This configmap contains the "cluster ID" and "account ID" required for the CSI Driver to function properly.
7. The cluster should have the `storage-secret-store` secret created in the same namespace as your manifests are applied. This secret contains the "IBM Cloud API Key" required for the CSI Driver to function properly.

Note: More details about steps 5, 6, and 7 can be found in the [Apply manifests](#apply-manifests) section below.

## Apply manifests

- The repo uses kustomize to manage the deployment manifests.
- The deployment manifests are available in the `deploy/kubernetes/manifests` folder.
- The deployment manifests are organized in overlays for different environments such as `dev`, `stage`, `stable`, and `release`. But for now we **only maintain** `dev` (used for development and testing purposes)
- The `deploy/kubernetes/deploy-vpc-file-driver.sh` script is used to apply manifests on the targeted cluster. The script is capable of installing kustomize and using it to deploy the driver in the cluster. The script will use the `deploy/kubernetes/manifests/overlays/dev` folder by default, but can be used with other overlays as well going forward.

1. User needs to update all the values marked with `<UPDATE THIS>` in the `deploy/kubernetes/manifests/overlays/dev` folder, such as:
  - `slclient_gen2.toml`:
    - `g2_riaas_endpoint_url`: Infrastructure endpoint URL, ref, https://cloud.ibm.com/docs/vpc?topic=vpc-service-endpoints-for-vpc
    - `g2_resource_group_id`: Ref, https://cloud.ibm.com/docs/account?topic=account-rgs&interface=cli  
    - `g2_api_key`: Ref, https://cloud.ibm.com/docs/account?topic=account-userapikey&interface=cli
  - `kustomization.yaml`: 
    - `namespace`: The namespace to deploy the driver, such as `kube-system` or `openshift-cluster-csi-drivers`.
  - `cm-clusterInfo-data.yaml`: 
    - `cluster_id`: Obtain Cluster ID using `kubectl get nodes -l node-role.kubernetes.io/master --output json | jq -r '.items[0].metadata.name'`
    - `account_id`: Obtain IBM Cloud Account ID using `ibmcloud account show -o json | jq -r .account_id`
  - `cm-providerData-data.yaml`
    - `vpc_id`: Obtain VPC ID using `ibmcloud is vpcs`
    - `vpc_subnet_ids`: Obtain VPC Subnet IDs using `ibmcloud is subnets --vpc-id <vpc_id>`
  - `node-server-images.yaml` and `controller-server-images.yaml`: The container image to be used. Refer to the section [Image To be used](#image-to-be-used) above for more details on how to get the image tag.
  - `sa-controller-secrets.yaml` and `sa-node-secrets.yaml`: The image pull secret to be used in [Push container image to a container registry](#push-container-image-to-a-container-registry) section above.

2. Once all the values are added, user can run below command to deploy the driver in the cluster. This will run the `deploy-vpc-file-driver.sh` script with the `dev` overlay by default.
```shell
bash ./deploy/kubernetes/deploy-vpc-file-driver.sh
```

## Delete manifests

To delete the manifests applied in the cluster, you can use the `delete-vpc-file-driver.sh` script. This script will remove all the resources created by the `deploy-vpc-file-driver.sh` script.

```shell
bash ./deploy/kubernetes/delete-vpc-file-driver.sh
```

In case of OCP clusters, run additional command to set SecurityContextConstraints(SCC).
```shell
oc apply -f deploy/openshift/scc.yaml
```

## Testing and Troubleshooting
To test the deployment of the IBM Cloud File Storage Share CSI Driver, you can use the provided example manifests in the `examples/` folder. More details can be found in the [examples/README.md](examples/README.md) file.

For troubleshooting, use debug commands such as:

```shell
pod_controller=$(kubectl get pods --namespace kube-system | grep ibm-vpc-file-csi-controller | awk '{print $1}')
pod_node_sample=$(kubectl get pods --namespace kube-system | grep ibm-vpc-file-csi-node | awk '{print $1}' | head -n 1)

kubectl describe pod $pod_controller --namespace kube-system | grep Event -A 20
kubectl describe pod $pod_node_sample --namespace kube-system | grep Event -A 20

kubectl logs $pod_controller  --namespace kube-system --container csi-provisioner
kubectl logs $pod_controller  --namespace kube-system --container iks-vpc-file-driver
kubectl logs $pod_node_sample --namespace kube-system --container iks-vpc-file-node-driver
```

Note:
- You may need to change the namespace from `kube-system` to the namespace where you have deployed the driver.
- There are 2 replicas of the controller pod and the containers inside that pod have leader election enabled. The pods switch leadership based on leases and hence you may have to check both pods for logs and events.

## E2E Tests
Please refer [this](https://github.com/IBM/ibmcloud-volume-file-vpc/tree/master/e2e) repository for e2e tests.
