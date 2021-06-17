
# ibm-vpc-file-csi-driver

ibm-vpc-file-csi-driver is a CSI plugin for creating and mounting VPC File storage.
It includes both Controller and Node service



## Clone GHE repository

- Make a go workspace
  - `mkdir ibm-vpc-file-csi-driver-ws`

- Make the go folder structure and the initial src folders and clone the repository
  - `mkdir -p ibm-vpc-file-csi-driver-ws/src/github.com/IBM`
  - `cd ibm-vpc-file-csi-driver-ws/src/github.com/IBM/`
  - `git clone git@github.com:IBM/ibm-vpc-file-csi-driver.git`
  - `cd ibm-vpc-file-csi-driver`


## Build and Push image ( Optional  if you just want to use already available image --> registry.stage1.ng.bluemix.net/armada-master/ibm-vpc-file-csi-driver:latest )

- Set GOPATH
  - `export GOPATH= ibm-vpc-file-csi-driver-ws`
- Builds project and runs testcases.`
  - `make`
- Builds docker image
  - `make buildimage`
- Push image to registry
  - `PREFIX="docker.io/library" TAG="latest-amd64" IMAGENAME="ibm-vpc-file-csi-driver" REG="stg.icr.io/sameshai" && docker tag $PREFIX/$IMAGENAME $REG/$IMAGENAME && docker push $REG/$IMAGENAME`
  The image will be available as `stg.icr.io/sameshai/ibm-vpc-file-csi-driver:latest`


## Deploy CSI driver on your cluster
- Get cluster config
  - `ibmcloud cluster-config <cluster-name>`
- Export cluster config
  - ` export KUBECONFIG=<config-file-path>`
- Deploy CSI plugin on your cluster
  - Update the image tag ( Optional  if you just want to use already available image --> registry.stage1.ng.bluemix.net/armada-master/ibm-vpc-file-csi-driver:latest )
     - `sed -i 's/:latest/:mytest/g' deploy/kubernetes/driver/kubernetes/overlays/dev/controller-server-images.yaml`
	   - `sed -i 's/:latest/:mytest/g' deploy/kubernetes/driver/kubernetes/overlays/dev/node-server-images.yaml`
  - Install `kustomize` tool. The instructions are available [here](https://github.com/kubernetes-sigs/kustomize/blob/master/docs/INSTALL.md)
  - Deploy plugin
    - `sh deploy/kubernetes/driver/kubernetes/deploy-vpc-file-driver.sh stage`

## Testing
- Create storage classes
  - `ls deploy/kubernetes/storageclass/ | xargs -I classfile kubectl apply -f deploy/kubernetes/storageclass/classfile`
- Create PVC
  - `kubectl create -f examples/kubernetes/validPVC.yaml`
- .Create POD with volume
  - `kubectl create -f examples/kubernetes/validPOD.yaml`
