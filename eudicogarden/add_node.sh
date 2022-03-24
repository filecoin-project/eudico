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

echo [*] Getting genesis from bootstrap
BOOTSTRAP=`terraform output -raw eudico_bootstrap_ip`
LOTUS_PATH="~/.eudico"
scp -o "StrictHostKeyChecking no" ubuntu@$BOOTSTRAP:~/eudico/eudicogarden/eudicogarden.car .

LENGTH=`terraform output -json eudico_nodes_ip | jq -r length`
TOTAL_NODES=`expr $LENGTH + $NUM`
# echo [*] Provisioning infrastructure
# terraform validate
# terraform plan -out=plan.out -var="num_nodes=${TOTAL_NODES}"
# terraform apply plan.out

BOOTSTRAP_MADDR=`ssh -o "StrictHostKeyChecking no" ubuntu@$BOOTSTRAP "cd eudico && ./eudico net listen | head -n 1"`
echo [*] Initializing $NUM eudico-nodes
for((i=1;i<=$NUM;i++))
do
        # Start getting nodes from the end.
        IP=`terraform output -json eudico_nodes_ip | jq -r '.['"-$i"']'`
        INDEX=`expr $LENGTH - $i`
        echo "[*] Initializing node with IP: $IP"
        # TODO: Remove once this is merged in eudico's main branch
        scp -o "StrictHostKeyChecking no" -r ../eudicogarden ubuntu@$IP:~/eudico/eudicogarden
        scp -o "StrictHostKeyChecking no" eudicogarden.car ubuntu@$IP:~/eudico/eudicogarden
        # TODO: We need to initialize sectors and the miner (without genesis)
        ssh -o "StrictHostKeyChecking no" ubuntu@$IP "cd eudico/eudicogarden && ./init_new_node.sh $INDEX $BOOTSTRAP_MADDR"
done
