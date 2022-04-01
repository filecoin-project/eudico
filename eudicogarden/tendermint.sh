# This is a script ro run one-node Tendermint with Eudico ABCI application.
ABCI="tcp://0.0.0.0:26658"
RPC_ADDR="tcp://0.0.0.0:26657"
PROXY_ADDR="tcp://127.0.0.1:26658"

rm -rf /tmp/config
rm -rf /tmp/data

if [ $# -eq 0 ]; then
 echo "Please use start or stop commands"
 exit 1
fi

if [ $1 == "stop" ]; then
  tmux kill-session -t tendermint
  exit 0
fi

if [ $1 == "start" ]; then

tmux new-session -d -s "tendermint" \; \
  new-window   -t "tendermint" \; \
  split-window -t "tendermint:0" -v \; \
  \
  send-keys -t "tendermint:0.0" "./eudico tendermint application -addr=$ABCI & " Enter \; \
  send-keys -t "tendermint:0.1" "
        docker run --rm -v /tmp:/tendermint --entrypoint /usr/bin/tendermint  tendermint/tendermint:v0.35.1 init \
            validator --key secp256k1" Enter \; \
  send-keys -t "tendermint:0.1" "
        docker run --network=host --rm -v /tmp:/tendermint --entrypoint /usr/bin/tendermint tendermint/tendermint:v0.35.1 start \
            --rpc.laddr $RPC_ADDR \
            --proxy-app $PROXY_ADDR" Enter \; \
  attach-session -t "tendermint:0.1"
  exit 0
fi

echo "unknown command"
exit 1