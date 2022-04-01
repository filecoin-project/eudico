# This is a script for manual sanity check of one node Tendermint-Eudico integration within Eudico Garden setup.
# The sanity check is simple: you just have to run it and perform several Eudico commands like "send".

TM_PATH="/tmp"
ABCI_ADDR="tcp://0.0.0.0:26658"
NODE_ADDR="0.0.0.0:26657"
PROXY_ADDR="tcp://127.0.0.1:26658"
NODE_PATH="$HOME/.eudico-node0"
TM_RPC="http://127.0.0.1:26657"

# secp256k1 private keys
NODE_KEY="$TM_PATH/config/priv_validator_key.json"

NODE_DAEMON_LOG="./eudico_daemon_0.log"
NODE_MINER_LOG="./eudico_miner_0.log"

rm -rvf $NODE_PATH
rm -rf $TM_PATH/config
rm -rf $TM_PATH/data
rm -rf ./eudico_daemon_*.log
rm -rf ./eudico_miner_*.log

tmux new-session -d -s "tendermint" \; \
  new-window   -t "tendermint" \; \
  \
  split-window -t "tendermint:0" -v \; \
  split-window -t "tendermint:0.0" -h \; \
  split-window -t "tendermint:0.1" -h \; \
  \
  send-keys -t "tendermint:0.0" "./eudico tendermint application -addr=$ABCI_ADDR " Enter \; \
    send-keys -t "tendermint:0.1" "
          docker run --rm -v /tmp:/tendermint --entrypoint /usr/bin/tendermint  tendermint/tendermint:v0.35.1 init \
              validator --key secp256k1" Enter \; \
    send-keys -t "tendermint:0.1" "
          docker run --network=host --rm -v /tmp:/tendermint --entrypoint /usr/bin/tendermint tendermint/tendermint:v0.35.1 start \
              --rpc.laddr tcp://$NODE_ADDR \
              --proxy-app $PROXY_ADDR" Enter \; \
  \
  send-keys -t "tendermint:0.2" "
        export EUDICO_TENDERMINT_RPC=$TM_RPC
        export EUDICO_PATH=$NODE_PATH
        ./eudico tendermint daemon --genesis=./testdata/tendermint.gen 2>&1 | tee $NODE_DAEMON_LOG" Enter \; \
  send-keys -t "tendermint:0.3" "
        export EUDICO_TENDERMINT_RPC=$TM_RPC
        export EUDICO_PATH=$NODE_PATH
        sleep 5
        ./eudico wait-api;
        ./eudico wallet import-tendermint-key --as-default -path=$NODE_KEY; sleep 2;
        ./eudico tendermint miner --default-key 2>&1 | tee $NODE_MINER_LOG" Enter \; \
  attach-session -t "tendermint:0.3"