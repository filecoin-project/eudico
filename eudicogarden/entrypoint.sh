#!/bin/bash
sudo apt-get update
sudo apt install mesa-opencl-icd ocl-icd-opencl-dev gcc git bzr jq pkg-config curl clang build-essential hwloc libhwloc-dev wget zip unzip -y && sudo apt upgrade -y
wget -c https://golang.org/dl/go1.16.4.linux-amd64.tar.gz -O - | sudo tar -xz -C /usr/local
echo "export PATH=$PATH:/usr/local/go/bin" >> ~/.bashrc && source ~/.bashrc
echo "ulimit -n 100000" >> ~/.bashrc && source ~/.bashrc
echo 'export LOTUS_PATH=~/.eudico
export LOTUS_MINER_PATH=~/.lotusminer
export LOTUS_SKIP_GENESIS_CHECK=_yes_
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"
export LOTUS_IGNORE_DRAND=_yes_
export LOTUS_LIBP2P_LISTENADDRESSES="/ip4/0.0.0.0/tcp/30201"' >> ~/.bashrc && source ~/.bashrc
# Clone eudico
git clone https://github.com/filecoin-project/eudico.git
cd eudico

export LOTUS_PATH=~/.eudico
export LOTUS_MINER_PATH=~/.lotusminer
export LOTUS_SKIP_GENESIS_CHECK=_yes_
export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
export CGO_CFLAGS="-D__BLST_PORTABLE__"
export LOTUS_IGNORE_DRAND=_yes_
export LOTUS_LIBP2P_LISTENADDRESSES="/ip4/0.0.0.0/tcp/30201"
export PATH=$PATH:/usr/local/go/bin
ulimit -n 100000
make 2k
./lotus fetch-params 2048
