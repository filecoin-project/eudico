# Eudico paths
NODE_0_PATH="$HOME/.eudico-node0"

NODE_0_DAEMON_LOG="./eudico_daemon_0.log"
NODE_0_MINER_LOG="./eudico_miner_0.log"

LOG_LEVEL="info,mir-consensus=debug,mir-agent=debug"
NODES=/root:t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@127.0.0.1:10000
NODE_0=/root:t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy

rm -rf ./eudico
make eudico

rm -rvf $NODE_0_PATH
rm -rf ./eudico_daemon_*.log
rm -rf ./eudico_miner_*.log

tmux new-session -d -s "mir" \; \
  new-window   -t "mir" \; \
  \
  split-window -t "mir:0" -v \; \
  \
  send-keys -t "mir:0.0" "
        export EUDICO_PATH=$NODE_0_PATH
        export GOLOG_LOG_LEVEL=$LOG_LEVEL
        ./eudico mir daemon --genesis=./testdata/mir.gen 2>&1 | tee $NODE_0_DAEMON_LOG" Enter \; \
  send-keys -t "mir:0.1" "
        export EUDICO_PATH=$NODE_0_PATH
        export GOLOG_LOG_LEVEL=$LOG_LEVEL
        export EUDICO_MIR_ID=$NODE_0
        export EUDICO_MIR_CLIENTS=$NODES
        sleep 3
        ./eudico wait-api;
        ./eudico wallet import --as-default ./testdata/wallet/node0.key;
        ./eudico mir miner --default-key 2>&1 | tee $NODE_0_MINER_LOG" Enter \; \
  attach-session -t "mir:0.1"
