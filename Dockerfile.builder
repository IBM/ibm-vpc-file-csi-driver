<<<<<<< HEAD
FROM golang:1.20.11
=======
FROM golang:1.19
>>>>>>> 3e7b23c (Review Comments)

WORKDIR /go/src/github.com/IBM/ibm-vpc-file-csi-driver
ADD . /go/src/github.com/IBM/ibm-vpc-file-csi-driver


ARG TAG
ARG OS
ARG ARCH

CMD ["./scripts/build-bin.sh"]
