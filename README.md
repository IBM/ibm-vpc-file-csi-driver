# ibm-vpc-file-csi-driver

[![Build Status](https://api.travis-ci.com/IBM/ibm-vpc-file-csi-driver.svg?branch=master)](https://travis-ci.com/IBM/ibm-vpc-file-csi-driver)
[![Coverage](https://github.com/IBM/ibm-vpc-file-csi-driver/blob/gh-pages/coverage/master/badge.svg)](https://github.com/IBM/ibm-vpc-file-csi-driver/tree/gh-pages/coverage/master/cover.html)

The `ibm-vpc-file-csi-driver` is a CSI plugin for dynamic creation and mount of IBM Cloud File Storage Shares.

This driver includes both Controller and Node service

## Supported features

1. Dynamic PVC/PV creation and deletion with ReadWriteMany capability
2. POD creation and deletion which will mount/unmount the file storage volumes.
3. Defining custom storage class by providing gid/uid will allow non-root users access to file storage volumes.

## Supported platforms

The following are the supported Kubernetes platforms suitable for deployment for IBM Cloud File Storage Share CSI Driver:

|Kubernetes platform|Version|Architecture|
|----------------------|-------|------------|
|Red Hat® OpenShift®|4.6|x86_64|
|Red Hat® OpenShift®|4.7|x86_64|
|Red Hat® OpenShift®|4.8|x86_64|
|Red Hat® OpenShift®|4.9|x86_64|
|Kubernetes| 1.19|x86_64|
|Kubernetes| 1.20|x86_64|
|Kubernetes| 1.21|x86_64|
|Kubernetes| 1.22|x86_64|
|Kubernetes| 1.23|x86_64|

## How to contribute to the IBM Cloud File Storage Share CSI Driver

If you have any questions or issues you can create a new [GitHub Issue](https://github.com/IBM/ibm-vpc-file-csi-driver/issues/new) in this repository.

Pull requests are very welcome! Make sure your patches are well tested. Ideally create a topic branch for every separate change you make. For example:

1. Fork the repo
2. Create your feature branch (git checkout -b my-new-feature)
3. Commit your changes (git commit -am 'Added some feature')
4. Push to the branch (git push origin my-new-feature)
5. Create new Pull Request
6. Add the test results in the PR

## Prerequisites

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

For further details on IBM Cloud Infrastructure services with VPC networking and the IBM Cloud File Storage Share service, please refer below documentation:

[cloud.ibm.com/docs/vpc?topic=vpc-file-storage-vpc-about](https://cloud.ibm.com/docs/vpc?topic=vpc-file-storage-vpc-about)

---

## Deploy IBM Cloud File Storage Share CSI Driver

This will use an existing container image release of the IBM Cloud File Storage Share CSI Driver, using a pre-defined Kustomization `stable` overlay.

### Confirm network security

To confirm the VPC Security Group Rules for the cluster, use the following script to test if the Security Groups attached to a worker node contain any rules that permit NFS protocol port 2049 TCP:

```shell
ibmcloud plugin install -f is

vpc_name=""
worker_name="worker-1"
worker_instance_example=$(ibmcloud is instances | grep $vpc_name | grep $worker_name | awk '{print $1}')
worker_instance_sgs=$(ibmcloud is instance $worker_instance_example --output json | jq -r '.primary_network_interface.security_groups | .[].id')
sg_rules_list=""

for sg in ${worker_instance_sgs} ; do
  rules=$(ibmcloud is sg $sg --output json | jq -r '.rules | .[] | select(.protocol=="tcp") | [.port_min,.port_max] | @csv' | sort | uniq)
  sg_rules_list+=$'\n'"${rules}"
done

for rule in $(echo "$sg_rules_list" | grep "\S" | sort | uniq) ; do
  rule_min="${rule%%,*}"
  rule_max="${rule#*,}"
  if [ "2049" -ge "$rule_min" ] && [ "2049" -le "$rule_max" ]; then echo "NFS protocol port 2049 TCP is allowed" && export SG_RULE_FLAG="1" ; fi
done
if [ $SG_RULE_FLAG != "1" ]; then echo "Please add inbound/outbound SG Rule for 2049 TCP" ; fi
```

### Add additional labels to all worker nodes

Confirm the worker node labels as shown in the [Prerequisites](#prerequisites):

```shell
cluster_nodes=$(oc get nodes --output json | jq '.items[] | .metadata.labels')
echo "$cluster_nodes" | grep 'kubernetes.io/region\|kubernetes.io/zone'
echo "$cluster_nodes" | grep 'ibm-cloud.kubernetes.io/vpc-instance-id'
```

Add the additional labels as necessary to the worker nodes:
```shell
vpc_id=""
kubectl label nodes --selector node-role.kubernetes.io/worker "ibm-cloud.kubernetes.io/vpc-instance-id=$vpc_id"
```

### Install Kustomize (K8S native configuration management)

See instructions for installing Kustomize on [kubectl.docs.kubernetes.io/installation/kustomize](https://kubectl.docs.kubernetes.io/installation/kustomize).

Kustomize will use a fixed set of YAML configuration files known as `manifests` and will patch these files with different values from `overlays` subdirectories (such as dev, stage, stable, release).

#### Kustomize installation example:

```shell
# Download Kustomize (K8S native configuration management) precompiled binary to the current directory
curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh" | sh

# Alternatively, Download and version-lock Kustomize (K8S native configuration management) precompiled binary to the current directory
# curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh" > install_kustomize.sh
# chmod +x install_kustomize.sh
# ./install_kustomize.sh 5.5.0

# Move binary to PATH
mv ./kustomize /usr/bin/kustomize

# Ensure configuration file is loaded for Kubectl / OpenShift CLI
export KUBECONFIG=""
```

### Execute Deployment of the IBM Cloud File Storage Share CSI Driver

```shell
# Clone the repository
git clone https://github.com/IBM/ibm-vpc-file-csi-driver.git

# For IBM Cloud Kubernetes Service (IKS) use 'kubernetes' subdirectory
# For Red Hat OpenShift Container Platform (OCP) Self-Managed use 'openshift-self-managed' subdirectory
subdirectory_target="kubernetes"
# subdirectory_target="openshift-self-managed"

# Open subdirectory with deployment files
cd ./ibm-vpc-file-csi-driver/deploy/$subdirectory_target/driver/kubernetes

# Execute default stable version
./deploy-vpc-file-driver.sh

# The script will execute 'kustomize build' and use the given release with the overlays subdirectory
# This will on-the-fly alter the manifests subdirectory
# and pipe the resulting YAML deployment configuration to 'kubectl apply -f -'

# Optionally, for IKS confirm the deployment was successful
# kubectl get deployment ibm-vpc-file-csi-controller --namespace kube-system -o yaml
# kubectl get daemonset ibm-vpc-file-csi-node --namespace kube-system --output yaml

# Optionally, for OCP confirm the deployment was successful
# ocp_cluster_namespace="openshift-cluster-csi-drivers"
# oc get deployment ibm-vpc-file-csi-controller --namespace $ocp_cluster_namespace --output yaml
# oc get daemonset ibm-vpc-file-csi-node --namespace openshift-cluster-csi-drivers --output yaml
```

---

## Manual build and deploy

The following instructions describe a generic manual build and deploy of the IBM Cloud File Storage Share CSI Driver; after the manual build and upload of the container image to a Container Registry, the deploy instructions are based upon using the pre-defined Kustomization `stable` overlay.

### Confirm network security

To confirm the VPC Security Group Rules for the cluster, use the following script to test if the Security Groups attached to a worker node contain any rules that permit NFS protocol port 2049 TCP:

```shell
ibmcloud plugin install -f is

vpc_name=""
worker_name="worker-1"
worker_instance_example=$(ibmcloud is instances | grep $vpc_name | grep $worker_name | awk '{print $1}')
worker_instance_sgs=$(ibmcloud is instance $worker_instance_example --output json | jq -r '.primary_network_interface.security_groups | .[].id')
sg_rules_list=""

for sg in ${worker_instance_sgs} ; do
  rules=$(ibmcloud is sg $sg --output json | jq -r '.rules | .[] | select(.protocol=="tcp") | [.port_min,.port_max] | @csv' | sort | uniq)
  sg_rules_list+=$'\n'"${rules}"
done

for rule in $(echo "$sg_rules_list" | grep "\S" | sort | uniq) ; do
  rule_min="${rule%%,*}"
  rule_max="${rule#*,}"
  if [ "2049" -ge "$rule_min" ] && [ "2049" -le "$rule_max" ]; then echo "NFS protocol port 2049 TCP is allowed" && export SG_RULE_FLAG="1" ; fi
done
if [ $SG_RULE_FLAG != "1" ]; then echo "Please add inbound/outbound SG Rule for 2049 TCP" ; fi
```

### Add additional labels to all worker nodes

Confirm the worker node labels as shown in the [Prerequisites](#prerequisites):

```shell
cluster_nodes=$(oc get nodes --output json | jq '.items[] | .metadata.labels')
echo "$cluster_nodes" | grep 'kubernetes.io/region\|kubernetes.io/zone'
echo "$cluster_nodes" | grep 'ibm-cloud.kubernetes.io/vpc-instance-id'
```

Add the additional labels as necessary to the worker nodes:
```shell
vpc_id=""
kubectl label nodes --selector node-role.kubernetes.io/worker "ibm-cloud.kubernetes.io/vpc-instance-id=$vpc_id"
```

### Install OS Package dependencies and version locking

There are a few OS Package dependencies that are likely already available on the host:
- `git`
- `docker`
- `make`
- `go`
- Optional: `gettext` when installing to OCP Self-Managed, deploy script uses envsubst
- Optional: `gcc` and `glibc-devel` (for full build and test)

For example on RHEL, install dependencies (except Go):
```shell
yum install -y git docker make
# yum install -y gettext gcc glibc-devel
```

Or alternatively, install directly from each website such as [docs.docker.com/install](https://docs.docker.com/install/).

For the Go dependency it is recommended to lock to a recent Go release instead of using an OS Package installation, in case of changes in the latest Go release that might interfere with the build.

At time of writing the driver was compiled with Go `1.22.0`. Locking to this version can be achieved using tools such as [goenv](https://github.com/go-nv/goenv). A brief example setup using `goenv`:

```shell
# Install goenv
if [ ! -d "$HOME/.goenv" ]; then git clone https://github.com/go-nv/goenv.git ~/.goenv ; fi
shell_config_file="$HOME/.bash_profile" # ~/.bash_rc
grep -qxF 'export GOENV_ROOT="$HOME/.goenv"' "$shell_config_file" || echo 'export GOENV_ROOT="$HOME/.goenv"' >> "$shell_config_file"
grep -qxF 'export PATH="$GOENV_ROOT/bin:$PATH"' "$shell_config_file" || echo 'export PATH="$GOENV_ROOT/bin:$PATH"' >> "$shell_config_file"
grep -qxF 'eval "$(goenv init -)"' "$shell_config_file" || echo 'eval "$(goenv init -)"' >> "$shell_config_file"
grep -qxF 'export PATH="$GOROOT/bin:$PATH"' "$shell_config_file" || echo 'export PATH="$GOROOT/bin:$PATH"' >> "$shell_config_file"
grep -qxF 'export PATH="$PATH:$GOPATH/bin"' "$shell_config_file" || echo 'export PATH="$PATH:$GOPATH/bin"' >> "$shell_config_file"

# Restart shell session vars
# This will re-import the vars in the child shell session where the script is running
source ~/.bash_profile

# Install Go version
goenv install 1.22.0 --skip-existing

# Set Go version for current shell session
goenv shell 1.22.0

# Ensure goenv set for current shell session by restart shell session vars and echo GOPATH
source ~/.bash_profile
echo $GOPATH
```

Using `goenv` will automatically handle environment variables for Go, otherwise please set the [`GOPATH` environment variable](https://github.com/golang/go/wiki/SettingGOPATH).

### Install Kustomize (K8S native configuration management)

See instructions for installing Kustomize on [kubectl.docs.kubernetes.io/installation/kustomize](https://kubectl.docs.kubernetes.io/installation/kustomize).

Kustomize will use a fixed set of YAML configuration files known as `manifests` and will patch these files with different values from `overlays` subdirectories (such as dev, stage, stable, release).

#### Kustomize installation example:

```shell
# Download Kustomize (K8S native configuration management) precompiled binary to the current directory
curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh" | sh

# Alternatively, Download and version-lock Kustomize (K8S native configuration management) precompiled binary to the current directory
# curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh" > install_kustomize.sh
# chmod +x install_kustomize.sh
# ./install_kustomize.sh 5.5.0

# Move binary to PATH
mv ./kustomize /usr/bin/kustomize

# Ensure configuration file is loaded for Kubectl / OpenShift CLI
export KUBECONFIG=""
```

### Clone repository into build files path

The following example assumes using the primary repository, however this can also use any forked repository.

```shell
# Create path to build files
mkdir -p $GOPATH/src/github.com/IBM
cd $GOPATH/src/github.com/IBM/
if [ ! -d "$GOPATH/src/github.com/IBM/ibm-vpc-file-csi-driver" ]; then git clone https://github.com/IBM/ibm-vpc-file-csi-driver.git ; fi
```

### Pull docker image for creating the container image build

```shell
# Get docker container for golang
docker pull docker://docker.io/library/golang
```

On RHEL where `podman` is an alias of `docker`, this may require:
```shell
# Create alias for container image shortname in Makefile https://www.redhat.com/sysadmin/container-image-short-names
grep -qxF '"golang"="docker.io/library/golang"' /etc/containers/registries.conf || 
cat <<EOF >> /etc/containers/registries.conf
[aliases]
"golang"="docker.io/library/golang"
EOF
```

### Build and run tests

```shell
# Go to build files subdir and make
cd $GOPATH/src/github.com/IBM/ibm-vpc-file-csi-driver
make
```

### Build container image of the CSI Driver

```shell
# Go to build files subdir and use make to build container image
# Will run docker build --tag ibm-vpc-file-csi-driver:latest-$CPU_ARCH -f Dockerfile .
cd $GOPATH/src/github.com/IBM/ibm-vpc-file-csi-driver
make buildimage
```

### Push container image to a container registry

The container image should be pushed to any container registry that the cluster worker nodes have access/authorization to pull images from; these can be private or public, such as the public container registry [docker.io](https://hub.docker.com/).

More commonly the cluster running on IBM Cloud will leverage [IBM Cloud Container Registry](https://cloud.ibm.com/docs/Registry), and the container can be pushed under a private namespace.

It is recommended to create a IBM Cloud IAM Service ID with restricted authorizations for the cluster to access IBM Cloud Container Registry, and generate an API Key for Service ID before continuing. Please see the following:

- [Container Registry - Managing IAM access](https://cloud.ibm.com/docs/Registry?topic=Registry-iam&interface=cli)
- [IAM - Creating and working with Service IDs](https://cloud.ibm.com/docs/account?topic=account-serviceids&interface=cli)

The following example shows the sequence of commands to create the IBM Cloud Service ID for access from the Kubernetes cluster to IBM Cloud Container Registry:

```shell
ibmcloud_api_key=""
ibmcloud_service_id_name="service-id-k8s-cr"

# Login
ibmcloud login --no-region --apikey=$ibmcloud_api_key

# For using manual build of Container Image with IBM Cloud Container Registry
# Must use IAM Service ID
# Authorize Service ID with Service Access as Reader role for IBM Cloud Container Registry
ibmcloud iam service-id-create $ibmcloud_service_id_name --description 'Service ID to connect from Kubernetes cluster to IBM Cloud Container Registry'
ibmcloud iam service-policy-create $ibmcloud_service_id_name --roles Reader --service-name container-registry
ibmcloud iam service-api-key-create ${ibmcloud_service_id_name}-apikey $ibmcloud_service_id_name --description 'API Key of Service ID to connect from Kubernetes cluster to IBM Cloud Container Registry'
```

The following example shows the sequence of commands to push the built container image to IBM Cloud Container Registry:

```shell
ibmcloud_api_key=""
ibmcloud_resource_group_name=""
ibmcloud_region_name=""
ibmcloud_container_registry_namespace=""

# Login
ibmcloud login --no-region --apikey=$ibmcloud_api_key
ibmcloud target -r $ibmcloud_region_name

# Install CR plugin for IBM Cloud CLI
ibmcloud plugin install -f cr ks

# IBM Cloud Container Registry setup
# set to global for creating a global container namespace
# and local client Docker daemon login to IBM Cloud Container Registry using global API endpoint
ibmcloud cr region-set global
ibmcloud cr login --client docker

# Create Namespace in IBM Cloud Container Registry in Global
ibmcloud cr namespace-add $ibmcloud_container_registry_namespace -g $ibmcloud_resource_group_name

# Built Container Image is tagged under localhost repository using tag 'latest-$CPU_ARCH'
# Optionally, re-tag the build to just 'latest'
# docker image tag ibm-vpc-file-csi-driver:latest-$(go env GOARCH) icr.io/$ibmcloud_container_registry_namespace/ibm-vpc-file-csi-driver:latest

# Push the build
docker push ibm-vpc-file-csi-driver:latest-$(go env GOARCH) icr.io/$ibmcloud_container_registry_namespace/ibm-vpc-file-csi-driver:latest-$(go env GOARCH)

# Show the build Container Image in IBM Cloud Container Registry
ibmcloud cr image-list
```

### Create image pull secret in the cluster

The cluster requires an image pull secret to access the Container Registry and pull the container image. Examples below:

#### If using IBM Cloud Kubernetes Service (IKS):

```shell
iks_cluster_name=""
ibmcloud_service_id_api_key=""

# Make note of:
### docker-username: iamapikey
### docker-password:
### docker-email: iamapikey
ibmcloud ks cluster config --cluster $iks_cluster_name --admin

# Create image pull secret in the Cluster, must be named 'icr-io-secret'

kubectl create secret docker-registry icr-io-secret --namespace kube-system --docker-server=icr.io --docker-username=iamapikey --docker-password=$ibmcloud_service_id_api_key --docker-email=iamapikey
```

#### If using Red Hat OpenShift Container Platform (OCP) Self-Managed:

```shell
ocp_cluster_namespace="openshift-cluster-csi-drivers"
ibmcloud_service_id_api_key=""

# Ensure configuration file is loaded for Kubectl / OpenShift CLI
export KUBECONFIG=""

# Create image pull secret in the Cluster, must be named 'icr-io-secret'
oc create secret docker-registry icr-io-secret --namespace $ocp_cluster_namespace --docker-server=icr.io --docker-username=iamapikey --docker-password=$ibmcloud_service_id_api_key --docker-email=iamapikey

# Show image pull secret
# oc get secret icr-io-secret --namespace $ocp_cluster_namespace -o json
```

### Append the built Container Image

For the version selected (e.g. `stage`), the built Container Image tag and Container Registry needs to be injected into the Kustomize files.

This must be changed in 2 Kustomize overlay files.

```shell
# For IBM Cloud Kubernetes Service (IKS) use 'kubernetes' subdirectory
# For Red Hat OpenShift Container Platform (OCP) Self-Managed use 'openshift-self-managed' subdirectory
subdirectory_target="kubernetes"
# subdirectory_target="openshift-self-managed"

# Define variables
ibmcloud_api_key=""
ibmcloud_resource_group_name=""
ibmcloud_region_name=""
ibmcloud_container_registry_namespace=""

# Open subdirectory with the overlay files
cd $GOPATH/src/github.com/IBM/ibm-vpc-file-csi-driver/deploy/$subdirectory_target/driver/kubernetes
cd ./overlays/stable

# Inject container image build from the container registry into the deployment files
sed -i "s|icr.io/ibm/ibm-vpc-file-csi-driver.*|icr.io/$ibmcloud_container_registry_namespace/ibm-vpc-file-csi-driver:latest-$(go env GOARCH)|g" controller-server-images.yaml
sed -i "s|icr.io/ibm/ibm-vpc-file-csi-driver.*|icr.io/$ibmcloud_container_registry_namespace/ibm-vpc-file-csi-driver:latest-$(go env GOARCH)|g" node-server-images.yaml

# Return to original path
cd ../../

# Open manifests path
cd ./manifests

# Inject the secret reference to pull from the container registry
sed -i "s|# imagePullSecrets:|imagePullSecrets:|g" setup-vpc-file-sa.yaml
sed -i "s|#   - name: icr-io-secret|  - name: icr-io-secret|g" setup-vpc-file-sa.yaml

# Return to original path
cd ../

```

### Execute Deployment of the IBM Cloud File Storage Share CSI Driver

```shell
# For IBM Cloud Kubernetes Service (IKS) use 'kubernetes' subdirectory
# For Red Hat OpenShift Container Platform (OCP) Self-Managed use 'openshift-self-managed' subdirectory
subdirectory_target="kubernetes"
# subdirectory_target="openshift-self-managed"

# Open subdirectory with deployment files
cd $GOPATH/src/github.com/IBM/ibm-vpc-file-csi-driver/deploy/$subdirectory_target/driver/kubernetes

# Choose version of the driver to install - dev, stage, stable, release
# If no argument provided to script, will default to stable
./deploy-vpc-file-driver.sh stable

# The script will execute 'kustomize build' and use the given release with the overlays subdirectory
# This will on-the-fly alter the manifests subdirectory
# and pipe the resulting YAML deployment configuration to 'kubectl apply -f -'

# Optionally, for IKS confirm the deployment was successful
# kubectl get deployment ibm-vpc-file-csi-controller --namespace kube-system -o yaml

# Optionally, for OCP confirm the deployment was successful
# ocp_cluster_namespace="openshift-cluster-csi-drivers"
# oc get deployment ibm-vpc-file-csi-controller --namespace $ocp_cluster_namespace -o yaml
```

---

## Additional notes

### KMS Encryption

An IAM Service-to-Service Authorization is required, please see [Kubernetes service - Setting up KMS encryption for File Storage](https://cloud.ibm.com/docs/containers?topic=containers-storage-file-vpc-apps#storage-file-kms) and the below CLI command example:
```shell
# Must use IAM Service-to-Service Authorization
# Authorize source service IBM Cloud File Storage Shares to access target service IBM Cloud Key Protect, with at least Reader access to the KMS instance
ibmcloud iam authorization-policy-create is.share kms Reader --source-service-instance-id 123123 --target-service-instance-id 456456
```

---

## Testing and Troubleshooting

The following is a simplified smoke test.

#### If using IBM Cloud Kubernetes Service (IKS):

```shell
# Create Storage Class examples in default namespace
ls ./ibm-vpc-file-csi-driver/deploy/kubernetes/storageclass/ | xargs -I classfile kubectl apply -f ./ibm-vpc-file-csi-driver/deploy/kubernetes/storageclass/classfile

# Create PVC in default namespace
kubectl create -f examples/kubernetes/validPVC.yaml

# Create Pod with attached volume in default namespace
kubectl create -f examples/kubernetes/validPOD.yaml

# Execute a command in a Pod container
kubectl exec -it pod-name /bin/bash

# In Pod container terminal, show mount paths for NFS
    root@pod-name:/#  mount -l | grep nfs

# In Pod container terminal, create temporary file
    root@pod-name:/#  echo 'hi' >> /mount_path_of_nfs/test.txt
    root@pod-name:/#  cat /mount_path_of_nfs/test.txt
```

#### If using Red Hat OpenShift Container Platform (OCP) Self-Managed:

```shell
# Edit the Storage Class example
vi ./ibm-vpc-file-csi-driver/examples/openshift-self-managed/ibmc-vpc-file-dp2-StorageClass.yaml

# Create Storage Class in default namespace
oc apply -f ./ibm-vpc-file-csi-driver/examples/openshift-self-managed/ibmc-vpc-file-dp2-StorageClass.yaml

# Create PVC in default namespace
oc create -f ./ibm-vpc-file-csi-driver/examples/openshift-self-managed/test1-validPVC.yaml

# Show PVC
oc describe pvc test1-pvc-validate -n default
oc get pvc test1-pvc-validate -n default -o yaml

# Create Deployment, with attached volume using PVC in default namespace
oc create -f ./ibm-vpc-file-csi-driver/examples/openshift-self-managed/test2-validPOD.yaml

# Show Deployment
oc get deployment test1-pvc-pod-validate -n default -o yaml

# Show Deployment ReplicaSet
oc get replicaset -n default

# Show Deployment ReplicaSet Pod/s
oc get pod -n default

# Execute a command in a Pod container
oc exec -it test1-pvc-pod-validate-HASH -- /bin/bash

# In Pod container terminal, show mount paths for NFS
    root@pod-name:/#  mount -l | grep nfs

# In Pod container terminal, create temporary file
    root@pod-name:/#  echo 'hi' >> /mount_path_of_nfs/test.txt
    root@pod-name:/#  cat /mount_path_of_nfs/test.txt
```

For troubleshooting, use debug commands such as:

```shell
pod_controller=$(kubectl get pods --namespace openshift-cluster-csi-drivers | grep ibm-vpc-file-csi-controller | awk '{print $1}')
pod_node_sample=$(kubectl get pods --namespace openshift-cluster-csi-drivers | grep ibm-vpc-file-csi-node | awk '{print $1}' | head -n 1)

oc describe pod $pod_controller --namespace openshift-cluster-csi-drivers | grep Event -A 20
oc describe pod $pod_node_sample --namespace openshift-cluster-csi-drivers | grep Event -A 20

oc logs $pod_controller  --namespace openshift-cluster-csi-drivers --container csi-provisioner
oc logs $pod_controller  --namespace openshift-cluster-csi-drivers --container iks-vpc-file-driver
oc logs $pod_node_sample --namespace openshift-cluster-csi-drivers --container iks-vpc-file-node-driver
```

---

## Delete IBM Cloud File Storage Share CSI Driver from the cluster

To perform deletion of all objects for the IBM Cloud File Storage Share CSI Driver, use:

```shell
./ibm-vpc-file-csi-driver/deploy/kubernetes/driver/kubernetes/delete-vpc-csi-driver.sh stage
```

For OCP Self-Managed, also delete the prerequisite config data files:
```shell
oc delete configmap ibm-cloud-provider-data --namespace openshift-cluster-csi-drivers
oc delete configmap cluster-info --namespace openshift-cluster-csi-drivers
```
