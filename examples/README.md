# Usage
This document provides examples of how to use the Kubernetes CSI driver for managing storage resources. The examples include creating Persistent Volume Claims (PVCs), deploying applications, and accessing file shares.

## Dynamic Provisioning (using default storage class)
```sh
kubectl apply -f examples/kubernetes/file-share-pvc.yaml
kubectl apply -f examples/kubernetes/file-share-deploy.yaml
```

## Create PVC with custom parameters
You can create a Persistent Volume Claim (PVC) with custom parameters by defining a storage class and specifying the parameters in the PVC definition.

1. Fill storage class YAML file, for example [examples/kubernetes/my-storageclass.yaml](./my-storageclass.yaml) and apply it in your cluster.
```sh
kubectl apply -f examples/kubernetes/my-storageclass.yaml
```
2. Create PVC like shown above but changing the `storageClassName` to the name of your storage class.
3. Use PVC in your application deployment and apply in your cluster.

#### Access File Share
Verify the PVC is created and bound to a Persistent Volume (PV):
```sh
$ kubectl get pvc
NAME            STATUS    VOLUME   CAPACITY   ACCESS MODES   STORAGECLASS                VOLUMEATTRIBUTESCLASS   AGE
file-share-pvc  Bound     pvc-xxx   10Gi        RWO            ibmc-vpc-file-5iops-tier   <unset>                 3m57s
```

Verify the application pod is running and exec:
```sh
$ kubectl exec -it file-share-pod -- df -h
Filesystem      Size  Used Avail Use% Mounted on
/dev/mapper/ibm--vpc--file--5iops--tier-pvc-xxx  10Gi   20M  9.8Gi   1% /mnt/file-share
```

## Static Provisioning
To statically provision a Persistent Volume (PV) and use it in your application, follow the public doc [Attaching existing file storage to an app](https://cloud.ibm.com/docs/containers?topic=containers-storage-file-vpc-apps#vpc-add-file-static).

## StorageClass secret
We can use the storage class secret to overwrite the default values of storageClass parameters. The example below will show how to specify your PVC settings in a Kubernetes secret and reference this secret in a customized storage class. Then, use the customized storage class to create a PVC with the custom parameters that you set in your secret.

### Enabling every user to customize the default PVC settings

1. In your storage class YAML file [examples/kubernetes/SCS-storageclass.yaml](./SCS-storageclass.yaml), reference the Kubernetes secret in the `parameters` section as follows. Make sure to add the code as-is and not to change variables names.

```
csi.storage.k8s.io/provisioner-secret-name: ${pvc.name}
csi.storage.k8s.io/provisioner-secret-namespace: ${pvc.namespace}
```

Following parameters can be overwritten using the storageclass secret,

```
1. iops
2. zone
3. tags
4. encrypted
5. resourceGroup
6. encryptionKey
```

2. As the cluster user, create a Kubernetes secret like [examples/kubernetes/SCS-secret.yaml](./SCS-secret.yaml) which has all the possible parameters that can be overwritten.
```sh
$ kubectl apply -f examples/kubernetes/SCS-secret.yaml
```
3. Create PVC like [examples/kubernetes/SCS-pvc.yaml](./SCS-pvc.yaml)

Make sure to create the PVC with the same name as used for storageclass-secret. Using the same name for the secret and the PVC triggers the storage provider to apply the settings of the secret in your PVC.
