FROM golang:1.17.9-buster

RUN useradd -m lotus && \
    apt-get update -y && \
    apt install -y mesa-opencl-icd ocl-icd-opencl-dev gcc git bzr jq pkg-config curl clang build-essential hwloc libhwloc-dev wget && \
    apt upgrade -y

COPY . .

RUN make eudico