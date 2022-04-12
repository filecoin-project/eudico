TM_PATH="/tmp"
ABCI_0="tcp://0.0.0.0:26658"
NODE_0="0.0.0.0:26657"
PROXY_ADDR="tcp://host.docker.internal:26658"

# Eudico paths
NODE_0_PATH="$HOME/.eudico-node0"

# secp256k1 private keys
NODE_0_KEY="$TM_PATH/config/priv_validator_key.json"

NODE_0_DAEMON_LOG="./eudico_daemon_0.log"
NODE_0_MINER_LOG="./eudico_miner_0.log"

#rm -rf ./eudico
#make eudico

rm -rvf $NODE_0_PATH
rm -rf $TM_PATH/config
rm -rf $TM_PATH/data
rm -rf ./eudico_daemon_*.log
rm -rf ./eudico_miner_*.log

tmux new-session -d -s "tendermint" \; \
  new-window   -t "tendermint" \; \
  \
  split-window -t "tendermint:0" -v \; \
  \
  send-keys -t "tendermint:0.0" "./eudico tendermint application -addr=$ABCI_0 " Enter \; \
    send-keys -t "tendermint:0.1" "
          docker run --rm -v /tmp:/tendermint --entrypoint /usr/bin/tendermint  tendermint/tendermint:v0.35.1 init \
              validator --key secp256k1" Enter \; \
    send-keys -t "tendermint:0.1" "
          docker run -p 0.0.0.0:26657:26657/tcp --rm -v /tmp:/tendermint --entrypoint /usr/bin/tendermint tendermint/tendermint:v0.35.1 start \
              --rpc.laddr tcp://$NODE_0 \
              --proxy-app $PROXY_ADDR" Enter \; \
  \
  attach-session -t "tendermint:0.1"