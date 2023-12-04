#FROM alpine:3.7
ARG PROXY_IMAGE_URL=blank
FROM ubuntu:16.04

# Default values
ARG git_commit_id=unknown
ARG git_remote_url=unknown
ARG build_date=unknown
ARG jenkins_build_number=unknown
ARG REPO_SOURCE_URL=blank
ARG BUILD_URL=blank

# Add Labels to image to show build details
LABEL git-commit-id=${git_commit_id}
LABEL git-remote-url=${git_remote_url}
LABEL build-date=${build_date}
LABEL jenkins-build-number=${jenkins_build_number}
LABEL razee.io/source-url="${REPO_SOURCE_URL}"
LABEL razee.io/build-url="${BUILD_URL}"
LABEL compliance.owner="ibm-armada-storage"

RUN apt-get update && apt-get install -y --no-install-recommends nfs-common && \
   apt-get install -y udev && \		
         apt-get install -y --no-install-recommends apt && \		
 	apt-get install -y --no-install-recommends ca-certificates xfsprogs && \		
 	apt-get upgrade -y && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /home/ibm-csi-drivers/
ADD ibm-vpc-file-csi-driver /home/ibm-csi-drivers
RUN chmod +x /home/ibm-csi-drivers/ibm-vpc-file-csi-driver

USER 2121:2121

ENTRYPOINT ["/home/ibm-csi-drivers/ibm-vpc-file-csi-driver"]
