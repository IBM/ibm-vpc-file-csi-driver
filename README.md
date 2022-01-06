# ibm-vpc-file-csi-driver

# Supported orchestration platforms

The following are the supported orchestration platforms suitable for deployment for IBM VPC file CSI Driver.

|Orchestration platform|Version|Architecture|
|----------------------|-------|------------|
|Red Hat® OpenShift®|4.6|x86|
|Red Hat® OpenShift®|4.7|x86|
|Red Hat® OpenShift®|4.8|x86|
|Kubernetes| 1.19|x86|
|Kubernetes| 1.20|x86|
|Kubernetes| 1.21|x86|
|Kubernetes| 1.22|x86|

# Prerequisites

Following are the prerequisites to use the IBM VPC file CSI Driver:

1. User should have either Red Hat® OpenShift® or kubernetes cluster on IBM VPC Gen 2 infrastructure.
2. Should have compatible orchestration platform.
3. Install and configure `ibmcloud is` CLI or get the required worker/node details by using [`IBM Cloud Console`](https://cloud.ibm.com)
4. Cluster's worker node should have following labels, if not please apply labels before deploying IBM VPC file CSI Driver.
```
"failure-domain.beta.kubernetes.io/region"
"failure-domain.beta.kubernetes.io/zone"
"topology.kubernetes.io/region"
"topology.kubernetes.io/zone"
```
# Build the driver

For building the driver `docker` and `GO` should be installed on the system

1. On your local machine, install [`docker`](https://docs.docker.com/install/) and [`Go`](https://golang.org/doc/install).
2. GO version should be >=1.16
3. Set the [`GOPATH` environment variable](https://github.com/golang/go/wiki/SettingGOPATH).
4. Build the driver image

   ## Clone the repo or your forked repo

   ```
   $ mkdir -p $GOPATH/src/github.com/IBM
   $ cd $GOPATH/src/github.com/IBM/
   $ git clone https://github.com/IBM/ibm-vpc-file-csi-driver.git
   $ cd ibm-vpc-file-csi-driver
   ```
   ## Build project and runs testcases

   ```
   $ make
   ```
   ## Build container image for the driver

   ```
   $ make buildimage
   ```

   ## Push image to registry

   Image should be pushed to any registry from which cluster worker nodes have access to pull

   You can push the driver image to [docker.io](https://hub.docker.com/)  registry or [IBM public registry](https://cloud.ibm.com/docs/Registry?topic=Registry-registry_overview#registry_regions_local) under your namespace.

   For pushing to IBM registry:

   Create an image pull secret in your cluster

   1. ibmcloud login to the target region

   2. Run - ibmcloud cr region-set global

   3. Run - ibmcloud cr login

   4. Run - ibmcloud ks cluster config --cluster \<cluster-name\> --admin

   5. Review and retrieve the following values for your image pull secret.

      `<docker-username>` - Enter the string: `iamapikey`.

      `<docker-password>` - Enter your IAM API key. For more information about IAM API keys, see [ Understanding API keys ](https://cloud.ibm.com/docs/account?topic=account-manapikey).

      `<docker-email>` - Enter the string: iamapikey.

   6. Run the following command to create the image pull secret in your cluster. Note that your secret must be named icr-io-secret


      ```

       kubectl create secret docker-registry icr-io-secret --docker-server=icr.io --docker-username=iamapikey --docker-password=-<iam-api-key> --docker-email=iamapikey -n kube-system

      ```

# Deploy CSI driver on your cluster

IBM VPC endpoints which supports Gen2 is documented [here](https://cloud.ibm.com/docs/vpc?topic=vpc-service-endpoints-for-vpc)
- Install `kustomize` tool. The instructions are available [here](https://kubectl.docs.kubernetes.io/installation/kustomize/)
- Export cluster config i.e configuring kubectl command
- Deploy IBM VPC file CSI Driver on your cluster
  - You can use any overlays available under `deploy/kubernetes/driver/kubernetes/overlays/` and edit the image tag if you want to use your own build image from this source code, although defualt overalys are already using released IBM VPC file CSI Driver image 
	
  - Example using `stage` overlay to update the image tag
     - Change `iks-vpc-file-driver` image name in `deploy/kubernetes/driver/kubernetes/overlays/stage/controller-server-images.yaml`
     - Change `iks-vpc-file-driver` image name in `deploy/kubernetes/driver/kubernetes/overlays/stage/node-server-images.yaml`
  - Deploy plugin
    - `bash deploy/kubernetes/driver/kubernetes/deploy-vpc-file-driver.sh stage`

## Testing

- Create storage classes
  - `ls deploy/kubernetes/storageclass/ | xargs -I classfile kubectl apply -f deploy/kubernetes/storageclass/classfile`
- Create PVC
  - `kubectl create -f examples/kubernetes/validPVC.yaml`
- Create POD with volume
  - `kubectl create -f examples/kubernetes/validPOD.yaml`

# Delete CSI driver from your cluster

  - Delete plugin
    - `bash deploy/kubernetes/driver/kubernetes/delete-vpc-csi-driver.sh stage`

# E2E Tests

TBD

# How to contribute

If you have any questions or issues you can create a new issue [ here ](https://github.com/IBM/ibm-vpc-file-csi-driver/issues/new).

Pull requests are very welcome! Make sure your patches are well tested. Ideally create a topic branch for every separate change you make. For example:

1. Fork the repo

2. Create your feature branch (git checkout -b my-new-feature)

3. Commit your changes (git commit -am 'Added some feature')

4. Push to the branch (git push origin my-new-feature)

5. Create new Pull Request

6. Add the test results in the PR

# Supported features

1. Dynamic PVC/PV creation and deletion with ReadWriteMany capability
2. POD creation and deletion which will mount/unmount the file storage volumes.
3. Defining custom storage class by providing gid/uid will allow non-root users access to file storage volumes.


# For more details on support of CLI and VPC IAAS layer please refer below documentation
https://cloud.ibm.com/docs/vpc?topic=vpc-file-storage-vpc-about