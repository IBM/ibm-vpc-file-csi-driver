FROM golang:1.20.11

WORKDIR /go/src/github.com/IBM/ibm-vpc-file-csi-driver
ADD . /go/src/github.com/IBM/ibm-vpc-file-csi-driver


ARG TAG
ARG OS
ARG ARCH

CMD ["./scripts/build-bin.sh"]
