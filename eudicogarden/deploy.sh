#!/bin/bash
echo [*] Cleaning previous assets
rm genesis-sector* eudicogarden.car genesis.json plan.out
set -e
if [ $# -ne 1 ]
  then
    echo "Provide the number of nodes to deploy as first argument"
    exit 1
fi
NUM=$1

echo [*] Creating genesis assets
./genesis.sh $NUM
echo [*] Provisioning infrastructure
terraform validate
terraform plan -out=plan.out -var="num_nodes=${NUM}"
terraform apply plan.out

echo [*] Initializing bootstrap
BOOTSTRAP=`terraform output -raw eudico_bootstrap_ip`
LOTUS_PATH="~/.eudico"
# TODO: Remove this line once this is merged in eudico's main branch
scp -o "StrictHostKeyChecking no" -r ../eudicogarden ubuntu@$BOOTSTRAP:~/eudico/eudicogarden
# ssh -o "StrictHostKeyChecking no" ubuntu@$BOOTSTRAP "cd eudico/eudicogarden && mkdir -p $LOTUS_PATH/keystore && chmod 0600 $LOTUS_PATH/keystore && LOTUS_PATH=~/.eudico ../lotus-shed keyinfo import bootstrap.keyinfo && ./start_bootstrap.sh"
ssh -o "StrictHostKeyChecking no" ubuntu@$BOOTSTRAP "cd eudico/eudicogarden && ./start_bootstrap.sh"

BOOTSTRAP_MADDR=`ssh -o "StrictHostKeyChecking no" ubuntu@$BOOTSTRAP "cd eudico && ./eudico net listen | head -n 1"`
echo [*] Initializing $NUM eudico-nodes
for((i=0;i<$NUM;i++))
do
        IP=`terraform output -json eudico_nodes_ip | jq -r '.['"$i"']'`
        echo "[*] Initializing node with IP: $IP"
        # TODO: Remove this line once this is merged in eudico's main branch
        scp -o "StrictHostKeyChecking no" -r ../eudicogarden ubuntu@$IP:~/eudico/eudicogarden
        scp -o "StrictHostKeyChecking no" eudicogarden.car genesis-sector* ubuntu@$IP:~/eudico/eudicogarden
        ssh -o "StrictHostKeyChecking no" ubuntu@$IP "cd eudico/eudicogarden && ./init.sh $i $BOOTSTRAP_MADDR"
done
