#!/bin/bash
set -e
if [ $# -ne 1 ]
  then
    echo "Provide the public key you want to grant ssh access to Eudico Garden nodes"
    exit 1
fi
LENGTH=`terraform output -json eudico_nodes_ip | jq -r length`
SSH_PUB=`cat $1`
for((i=1;i<=$LENGTH;i++))
do
        # Start getting nodes from the end.
        IP=`terraform output -json eudico_nodes_ip | jq -r '.['"-$i"']'`
        echo [*] Granting access to node $IP
        ssh -o "StrictHostKeyChecking no" ubuntu@$IP "echo $SSH_PUB >> .ssh/authorized_keys"
done
