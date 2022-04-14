# Eudico paths
NODE_0_PATH="$HOME/.eudico-node0"

NODE_0_DAEMON_LOG="./eudico_daemon_0.log"
NODE_0_MINER_LOG="./eudico_miner_0.log"

LOG_LEVEL="info,mir-consensus=debug"

rm -rf ./eudico
make eudico

rm -rvf $NODE_0_PATH
rm -rf ./eudico_daemon_*.log
rm -rf ./eudico_miner_*.log

tmux new-session -d -s "mirbft" \; \
  new-window   -t "mirbft" \; \
  \
  split-window -t "mirbft:0" -v \; \
  \
  send-keys -t "mirbft:0.0" "
        export EUDICO_PATH=$NODE_0_PATH
        export GOLOG_LOG_LEVEL=$LOG_LEVEL
        ./eudico mirbft daemon --genesis=./testdata/mirbft.gen 2>&1 | tee $NODE_0_DAEMON_LOG" Enter \; \
  send-keys -t "mirbft:0.1" "
        export EUDICO_PATH=$NODE_0_PATH
        export GOLOG_LOG_LEVEL=$LOG_LEVEL
        sleep 3
        ./eudico wait-api;
        ./eudico wallet import --as-default ./testdata/wallet/f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy.key;
        ./eudico mirbft miner --default-key 2>&1 | tee $NODE_0_MINER_LOG" Enter \; \
  attach-session -t "mirbft:0.1"
