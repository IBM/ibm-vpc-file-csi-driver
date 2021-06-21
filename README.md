
# ibm-vpc-file-csi-driver

ibm-vpc-file-csi-driver is a CSI plugin for creating and mounting VPC File storage.
It includes both Controller and Node service



# Supported orchestration platforms

The following table details orchestration platforms suitable for deployment of the IBM® VPC file storage CSI driver.

|Orchestration platform|Version|Architecture|
|----------------------|-------|------------|
|Kubernetes|1.20|x86|
|Kubernetes|1.21|x86|
|Kubernetes|1.19|x86|
|Red Hat® OpenShift®|4.7|x86|
|Red Hat OpenShift|4.6|x86|

# Prerequisites

To use the File Storage for VPC driver, complete the following tasks:
This conversation was marked as resolved by sameshai
 Show conversation

1. Create a cluster based on VPC infrastructure
2. Create Image pull secret in your cluster

   1. Review and retrieve the following values for your image pull secret.

      `<docker-username>` - Enter the string: `iamapikey`.

      `<docker-password>` - Enter your IAM API key. For more information about IAM API keys, see [ Understanding API keys ](https://cloud.ibm.com/docs/account?topic=account-manapikey).

      `<docker-email>` - Enter the string: iamapikey.

   2. Run the following command to create the image pull secret in your cluster. Note that your secret must be named icr-io-secret


      ```
    
       kubectl create secret docker-registry icr-io-secret --docker-server=icr.io --docker-username=iamapikey --docker-password=-<iam-api-key> --docker-email=iamapikey -n kube-system
      ```
      
# Build the driver

For building the driver `docker` and `GO` should be installed

1. On your local machine, install [`docker`](https://docs.docker.com/install/) and [`Go`](https://golang.org/doc/install).
2. Set the [`GOPATH` environment variable](https://github.com/golang/go/wiki/SettingGOPATH).
3. Build the driver image

   clone the repo or your forked repo
   ```
   $ mkdir -p $GOPATH/src/github.com/IBM
   $ mkdir -p $GOPATH/bin
   $ cd $GOPATH/src/github.com/IBM/
   $ git clone https://github.com/IBM/ibm-vpc-file-csi-driver.git
   $ cd ibm-vpc-file-csi-driver
   ```
   build project and runs testcases
   ```
   $ make
   ```
   build container image for the driver
   ```
   $ make buildimage
   ```

   Push image to registry

   Image should be pushed to any registry from which the worker nodes have access to pull

   1. You can push the driver image to [docker.io](https://hub.docker.com/)  registry or [IBM public registry](https://cloud.ibm.com/docs/Registry?topic=Registry-registry_overview#registry_regions_local) under your namespace.

# Deploy CSI driver on your cluster

- Export cluster config
- Deploy CSI plugin on your cluster
  - Update the image tag
     - Change `iks-vpc-file-driver` image name in `deploy/kubernetes/driver/kubernetes/overlays/stage/controller-server-images.yaml`
     - Change `iks-vpc-file-driver` image name in `deploy/kubernetes/driver/kubernetes/overlays/stage/node-server-images.yaml`
  - Install `kustomize` tool. The instructions are available [here](https://kubectl.docs.kubernetes.io/installation/kustomize/)
  - Deploy plugin
    - `sh deploy/kubernetes/driver/kubernetes/deploy-vpc-file-driver.sh stage`

## Testing
- Create storage classes
  - `ls deploy/kubernetes/storageclass/ | xargs -I classfile kubectl apply -f deploy/kubernetes/storageclass/classfile`
- Create PVC
  - `kubectl create -f examples/kubernetes/validPVC.yaml`
- .Create POD with volume
  - `kubectl create -f examples/kubernetes/validPOD.yaml`


# How to contribute

If you have any questions or issues you can create a new issue [ here ](https://github.com/IBM/ibm-vpc-file-csi-driver/issues/new).

Pull requests are very welcome! Make sure your patches are well tested. Ideally create a topic branch for every separate change you make. For example:

1. Fork the repo

2. Create your feature branch (git checkout -b my-new-feature)

3. Commit your changes (git commit -am 'Added some feature')

4. Push to the branch (git push origin my-new-feature)

5. Create new Pull Request


# Licensing

Copyright 2020 IBM Corp.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the License. You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific language governing permissions and limitations under the License.
