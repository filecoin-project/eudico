TENDERMINT_PATH="$HOME/Projects/tendermint"
NODE_0="127.0.0.1:26657"
NODE_1="127.0.0.1:26660"

NODE_0_PATH="$HOME/.eudico-node0"
NODE_1_PATH="$HOME/.eudico-node1"

NODE_0_NETADDR="$NODE_0_PATH/.netaddr"

NODE_0_API="1234"
NODE_1_API="1235"

NODE_0_KEY="$TENDERMINT_PATH/build/node0/config/priv_validator_key.json"
NODE_1_KEY="$TENDERMINT_PATH/build/node1/config/priv_validator_key.json"

NODE_1_APP_DATA="$TENDERMINT_PATH/build/node1/data/"

(cd "$TENDERMINT_PATH" && make localnet-stop)

#rm -rf ./eudico
#make eudico

rm -rvf ~/.eudico
rm -rvf ~/.eudico-node0
rm -rvf ~/.eudico-node1
rm -rvf ~/.eudico-node2
rm -rf $TENDERMINT_PATH/build/node*
rm -rf ./term*.log

tmux new-session -d -s "tendermint" \; \
  split-window -t "tendermint:0" -v -l 66% \; \
  split-window -t "tendermint:0.1" -v -l 50% \; \
  split-window -t "tendermint:0.1" -h \; \
  split-window -t "tendermint:0.3" -h \; \
  \
  send-keys -t "tendermint:0.0" "./eudico tendermint application -addr=tcp://0.0.0.0:26650 & " Enter \; \
  send-keys -t "tendermint:0.4" "./eudico tendermint application -addr=tcp://0.0.0.0:26651 > /dev/null 2>&1 & EUDICO_APP_PID=\$! " Enter \; \
  send-keys -t "tendermint:0.0" "./eudico tendermint application -addr=tcp://0.0.0.0:26652 & " Enter \; \
  send-keys -t "tendermint:0.0" "./eudico tendermint application -addr=tcp://0.0.0.0:26653 & " Enter \; \
  send-keys -t "tendermint:0.0" "cd $TENDERMINT_PATH; make localnet-start" Enter \; \
  \
  send-keys -t "tendermint:0.1" "
        export EUDICO_TENDERMINT_RPC=http://$NODE_0
        export EUDICO_PATH=$NODE_0_PATH
        ./scripts/wait-for-it.sh -t 0 $NODE_0 -- sleep 1;
        ./eudico tendermint daemon --genesis=./testdata/tendermint.gen --api=$NODE_0_API > term1.log 2>&1 &
            tail -f term1.log" Enter \; \
  send-keys -t "tendermint:0.2" "
        export EUDICO_TENDERMINT_RPC=http://$NODE_0
        export EUDICO_PATH=$NODE_0_PATH
        sleep 14
        ./eudico wait-api;
        ./eudico net listen | grep '/ip6/::1/' > $NODE_0_NETADDR
        ./eudico wallet import-tendermint-key --as-default -path=$NODE_0_KEY; sleep 2;
        ./eudico tendermint miner --default-key > term2.log 2>&1 &
            tail -f term2.log" Enter \; \
  send-keys -t "tendermint:0.3" "
          export EUDICO_TENDERMINT_RPC=http://$NODE_1
          export EUDICO_PATH=$NODE_1_PATH
          ./scripts/wait-for-it.sh -t 0 $NODE_1 -- sleep 1;
          ./eudico tendermint daemon --genesis=./testdata/tendermint.gen --api=$NODE_1_API > term3.log 2>&1 &
            tail -f term3.log" Enter \; \
  send-keys -t "tendermint:0.4" "
        alias stop-node1='(cd \"${TENDERMINT_PATH}\" && docker-compose stop node1)'
        alias start-node1='(cd \"${TENDERMINT_PATH}\" && docker-compose start node1)'
        alias start-app1='./eudico tendermint application -addr=tcp://0.0.0.0:26651 > /dev/null 2>&1 & EUDICO_APP_PID=\$! '
        alias stop-app1='kill -9 \$EUDICO_APP_PID'
        alias addr-node0='cat $NODE_0_NETADDR'; alias connect-node0='./eudico net connect \$(cat $NODE_0_NETADDR)'" Enter \; \
  send-keys -t "tendermint:0.4" "
        export EUDICO_TENDERMINT_RPC=http://$NODE_1; export EUDICO_PATH=$NODE_1_PATH
        sleep 14; ./eudico wait-api;
        ./eudico wallet import-tendermint-key --as-default -path=$NODE_1_KEY
        ./eudico tendermint miner --default-key > term4.log 2>&1 &
          tail -f term4.log" Enter \; \
  attach-session -t "tendermint:0.4"