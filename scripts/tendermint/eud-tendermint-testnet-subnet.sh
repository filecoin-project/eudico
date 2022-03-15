TENDERMINT_PATH="testdata/tendermint-testnet"

# Tendermint node RPC addresses
NODE_0="127.0.0.1:26657"
NODE_1="127.0.0.1:26660"
NODE_2="127.0.0.1:26662"
NODE_3="127.0.0.1:26664"

# Tendermint ABCI ports
ABCI_0="tcp://0.0.0.0:26650"
ABCI_1="tcp://0.0.0.0:26651"
ABCI_2="tcp://0.0.0.0:26652"
ABCI_3="tcp://0.0.0.0:26653"

# Eudico paths
NODE_0_PATH="$HOME/.eudico-node0"
NODE_1_PATH="$HOME/.eudico-node1"
NODE_2_PATH="$HOME/.eudico-node2"
NODE_3_PATH="$HOME/.eudico-node3"

NODE_0_NETADDR="$NODE_0_PATH/.netaddr"
NODE_1_NETADDR="$NODE_1_PATH/.netaddr"
NODE_2_NETADDR="$NODE_2_PATH/.netaddr"
NODE_3_NETADDR="$NODE_3_PATH/.netaddr"

# Eudico API ports
NODE_0_API="1234"
NODE_1_API="1235"
NODE_2_API="1236"
NODE_3_API="1237"

# secp256k1 private keys
NODE_0_KEY="$TENDERMINT_PATH/build/node0/config/priv_validator_key.json"
NODE_1_KEY="$TENDERMINT_PATH/build/node1/config/priv_validator_key.json"
NODE_2_KEY="$TENDERMINT_PATH/build/node2/config/priv_validator_key.json"
NODE_3_KEY="$TENDERMINT_PATH/build/node3/config/priv_validator_key.json"

NODE_0_DAEMON_LOG="./eudico_daemon_0.log"
NODE_0_MINER_LOG="./eudico_miner_0.log"
NODE_1_DAEMON_LOG="./eudico_daemon_1.log"
NODE_1_MINER_LOG="./eudico_miner_1.log"
NODE_2_DAEMON_LOG="./eudico_daemon_2.log"
NODE_2_MINER_LOG="./eudico_miner_2.log"
NODE_3_DAEMON_LOG="./eudico_daemon_3.log"
NODE_3_MINER_LOG="./eudico_miner_3.log"

NODE_1_APP_DATA="$TENDERMINT_PATH/build/node1/data/"

make tm-localnet-stop

rm -rf ./eudico
make eudico

rm -rvf $NODE_0_PATH
rm -rvf $NODE_1_PATH
rm -rvf $NODE_2_PATH
rm -rvf $NODE_3_PATH
rm -rf $TENDERMINT_PATH/build/node*
rm -rf ./eudico_daemon_*.log
rm -rf ./eudico_miner_*.log

tmux new-session -d -s "tendermint" \; \
  new-window   -t "tendermint" \; \
  split-window -t "tendermint:0" -v -l 66% \; \
  split-window -t "tendermint:0.1" -v -l 50% \; \
  split-window -t "tendermint:0.1" -h \; \
  split-window -t "tendermint:0.3" -h \; \
  \
  split-window -t "tendermint:1" -v \; \
  split-window -t "tendermint:1.0" -h \; \
  split-window -t "tendermint:1.2" -h \; \
  \
  send-keys -t "tendermint:0.0" "./eudico tendermint application -addr=$ABCI_0 & " Enter \; \
  send-keys -t "tendermint:0.4" "./eudico tendermint application -addr=$ABCI_1 &" Enter \; \
  send-keys -t "tendermint:0.0" "./eudico tendermint application -addr=$ABCI_2 & " Enter \; \
  send-keys -t "tendermint:0.0" "./eudico tendermint application -addr=$ABCI_3 & " Enter \; \
  send-keys -t "tendermint:0.0" "make tm-localnet-start" Enter \; \
  \
  send-keys -t "tendermint:0.1" "
        export EUDICO_TENDERMINT_RPC=http://$NODE_0
        export EUDICO_PATH=$NODE_0_PATH
        ./scripts/wait-for-it.sh -t 0 $NODE_0 -- sleep 1;
        ./eudico tspow daemon --genesis=./testdata/tspow.gen --api=$NODE_0_API 2>&1 | tee $NODE_0_DAEMON_LOG" Enter \; \
  send-keys -t "tendermint:0.2" "
        export EUDICO_TENDERMINT_RPC=http://$NODE_0
        export EUDICO_PATH=$NODE_0_PATH
        ./eudico wait-api;
        ./eudico net listen | grep '/ip6/::1/' > $NODE_0_NETADDR; sleep 2;
        ./eudico net connect \$(cat $NODE_2_NETADDR);
        ./eudico wallet import-tendermint-key --as-default -path=$NODE_0_KEY; sleep 30;
        ./eudico tspow miner --default-key 2>&1 | tee $NODE_0_MINER_LOG" Enter \; \
  send-keys -t "tendermint:0.3" "
          export EUDICO_TENDERMINT_RPC=http://$NODE_1
          export EUDICO_PATH=$NODE_1_PATH
          ./scripts/wait-for-it.sh -t 0 $NODE_1 -- sleep 1;
          ./eudico tspow daemon --genesis=./testdata/tspow.gen --api=$NODE_1_API 2>&1 | tee $NODE_1_DAEMON_LOG" Enter \; \
  send-keys -t "tendermint:0.4" "
        export EUDICO_TENDERMINT_RPC=http://$NODE_1; export EUDICO_PATH=$NODE_1_PATH;\
        ./eudico wait-api;\
        ./eudico net listen | grep '/ip6/::1/' > $NODE_1_NETADDR; sleep 2; \
        ./eudico net connect \$(cat $NODE_0_NETADDR);
        ./eudico wallet import-tendermint-key --as-default -path=$NODE_1_KEY; sleep 30; \
        ./eudico tspow miner --default-key 2>&1 | tee $NODE_1_MINER_LOG" Enter \; \
  \
  send-keys -t "tendermint:1.0" "
          export EUDICO_TENDERMINT_RPC=http://$NODE_2
          export EUDICO_PATH=$NODE_2_PATH
          ./scripts/wait-for-it.sh -t 0 $NODE_2 -- sleep 1;
          ./eudico tspow daemon --genesis=./testdata/tspow.gen --api=$NODE_2_API 2>&1 | tee $NODE_2_DAEMON_LOG" Enter \; \
    send-keys -t "tendermint:1.1" "
          export EUDICO_TENDERMINT_RPC=http://$NODE_2
          export EUDICO_PATH=$NODE_2_PATH
          ./eudico wait-api;
          ./eudico net listen | grep '/ip6/::1/' > $NODE_2_NETADDR; sleep 2;
          ./eudico net connect \$(cat $NODE_0_NETADDR);
          ./eudico wallet import-tendermint-key --as-default -path=$NODE_2_KEY; sleep 30;
          ./eudico tspow miner --default-key --default-key 2>&1 | tee $NODE_2_MINER_LOG" Enter \; \
    send-keys -t "tendermint:1.2" "
            export EUDICO_TENDERMINT_RPC=http://$NODE_3
            export EUDICO_PATH=$NODE_3_PATH
            ./scripts/wait-for-it.sh -t 0 $NODE_3 -- sleep 1;
            ./eudico tspow daemon --genesis=./testdata/tspow.gen --api=$NODE_3_API 2>&1 | tee $NODE_3_DAEMON_LOG" Enter \; \
    send-keys -t "tendermint:1.3" "
          export EUDICO_TENDERMINT_RPC=http://$NODE_3; export EUDICO_PATH=$NODE_3_PATH
          ./eudico wait-api;
          ./eudico net listen | grep '/ip6/::1/' > $NODE_3_NETADDR; sleep 2;
          ./eudico net connect \$(cat $NODE_2_NETADDR);
          ./eudico wallet import-tendermint-key --as-default -path=$NODE_3_KEY; sleep 30;
          ./eudico tspow miner --default-key --default-key 2>&1  | tee $NODE_3_MINER_LOG" Enter \; \
  attach-session -t "tendermint:0.4"
