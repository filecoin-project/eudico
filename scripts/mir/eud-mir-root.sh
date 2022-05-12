# Mir IDs
NODE_0=/root:t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy
NODE_1=/root:t1k7t2zufxvtgamk7ogoifa5mvdagb4cafu6pdzga
NODE_2=/root:t1rlhubezzmetmmpxyze22tc2uxuiiqv3iy6rvpra
NODE_3=/root:t1sqbkluz5elnekdu62ute5zjammslkplgdcpa2zi

# Persistent nodes in Tendermint-type format
NODES=/root:t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@127.0.0.1:10000,/root:t1k7t2zufxvtgamk7ogoifa5mvdagb4cafu6pdzga@127.0.0.1:10001,/root:t1rlhubezzmetmmpxyze22tc2uxuiiqv3iy6rvpra@127.0.0.1:10002,/root:t1sqbkluz5elnekdu62ute5zjammslkplgdcpa2zi@127.0.0.1:10003

# Eudico paths
NODE_0_PATH="$HOME/.eudico-node0"
NODE_1_PATH="$HOME/.eudico-node1"
NODE_2_PATH="$HOME/.eudico-node2"
NODE_3_PATH="$HOME/.eudico-node3"

NODE_0_KEY="./testdata/wallet/node0.key"
NODE_1_KEY="./testdata/wallet/node1.key"
NODE_2_KEY="./testdata/wallet/node2.key"
NODE_3_KEY="./testdata/wallet/node3.key"

NODE_0_NETADDR="$NODE_0_PATH/.netaddr"
NODE_1_NETADDR="$NODE_1_PATH/.netaddr"
NODE_2_NETADDR="$NODE_2_PATH/.netaddr"
NODE_3_NETADDR="$NODE_3_PATH/.netaddr"

# Eudico API ports
NODE_0_API="1234"
NODE_1_API="1235"
NODE_2_API="1236"
NODE_3_API="1237"

NODE_0_DAEMON_LOG="./eudico_daemon_0.log"
NODE_0_MINER_LOG="./eudico_miner_0.log"
NODE_1_DAEMON_LOG="./eudico_daemon_1.log"
NODE_1_MINER_LOG="./eudico_miner_1.log"
NODE_2_DAEMON_LOG="./eudico_daemon_2.log"
NODE_2_MINER_LOG="./eudico_miner_2.log"
NODE_3_DAEMON_LOG="./eudico_daemon_3.log"
NODE_3_MINER_LOG="./eudico_miner_3.log"

BLOCK0="./testdata/mir.gen"

rm -rf ./eudico-wal
rm -rf ./eudico
make eudico

rm -rvf $NODE_0_PATH
rm -rvf $NODE_1_PATH
rm -rvf $NODE_2_PATH
rm -rvf $NODE_3_PATH

rm -rvf $NODE_0_NETADDR
rm -rvf $NODE_1_NETADDR
rm -rvf $NODE_2_NETADDR
rm -rvf $NODE_3_NETADDR

rm -rf ./eudico_daemon_*.log
rm -rf ./eudico_miner_*.log

LOG_LEVEL="info,mir-consensus=debug,mir-agent=info"

tmux new-session -d -s "mir" \; \
  new-window   -t "mir" \; \
  split-window -t "mir:0" -v \; \
  split-window -t "mir:0.0" -h \; \
  split-window -t "mir:0.2" -h \; \
  \
  split-window -t "mir:1" -v \; \
  split-window -t "mir:1.0" -h \; \
  split-window -t "mir:1.2" -h \; \
  \
  send-keys -t "mir:0.0" "
        export GOLOG_LOG_LEVEL=$LOG_LEVEL
        export EUDICO_PATH=$NODE_0_PATH
        ./eudico mir daemon --genesis=$BLOCK0 --api=$NODE_0_API 2>&1 | tee $NODE_0_DAEMON_LOG" Enter \; \
  send-keys -t "mir:0.1" "
        export EUDICO_MIR_MINERS=$NODES
        export EUDICO_PATH=$NODE_0_PATH
        export GOLOG_LOG_LEVEL=$LOG_LEVEL
        ./eudico wait-api;
        ./eudico net listen | grep '/ip6/::1/' > $NODE_0_NETADDR; sleep 3;
        ./eudico wallet import --as-default $NODE_0_KEY
        ./eudico mir miner --default-key 2>&1 | tee $NODE_0_MINER_LOG" Enter \; \
  send-keys -t "mir:0.2" "
        export GOLOG_LOG_LEVEL=$LOG_LEVEL
        export EUDICO_PATH=$NODE_1_PATH
        ./eudico mir daemon --genesis=$BLOCK0 --api=$NODE_1_API 2>&1 | tee $NODE_1_DAEMON_LOG" Enter \; \
  send-keys -t "mir:0.3" "
        export GOLOG_LOG_LEVEL=$LOG_LEVEL
        export EUDICO_MIR_MINERS=$NODES
        export EUDICO_PATH=$NODE_1_PATH
        ./eudico wait-api;
        ./eudico net listen | grep '/ip6/::1/' > $NODE_1_NETADDR; sleep 3; \
        ./eudico net connect \$(cat $NODE_0_NETADDR);
        ./eudico wallet import --as-default $NODE_1_KEY
        ./eudico mir miner --default-key 2>&1 | tee $NODE_1_MINER_LOG" Enter \; \
  \
  send-keys -t "mir:1.0" "
        export GOLOG_LOG_LEVEL=$LOG_LEVEL
        export EUDICO_PATH=$NODE_2_PATH
        ./eudico mir daemon --genesis=$BLOCK0 --api=$NODE_2_API 2>&1 | tee $NODE_2_DAEMON_LOG" Enter \; \
    send-keys -t "mir:1.1" "
        export GOLOG_LOG_LEVEL=$LOG_LEVEL
        export EUDICO_MIR_MINERS=$NODES
        export EUDICO_PATH=$NODE_2_PATH
        ./eudico wait-api;
        ./eudico net listen | grep '/ip6/::1/' > $NODE_2_NETADDR; sleep 3;
        ./eudico net connect \$(cat $NODE_0_NETADDR);
        ./eudico wallet import --as-default $NODE_2_KEY
        ./eudico mir miner --default-key 2>&1 | tee $NODE_2_MINER_LOG" Enter \; \
    send-keys -t "mir:1.2" "
        export GOLOG_LOG_LEVEL=$LOG_LEVEL
        export EUDICO_PATH=$NODE_3_PATH
        ./eudico mir daemon --genesis=$BLOCK0 --api=$NODE_3_API 2>&1 | tee $NODE_3_DAEMON_LOG" Enter \; \
    send-keys -t "mir:1.3" "
       export GOLOG_LOG_LEVEL=$LOG_LEVEL
       export EUDICO_MIR_MINERS=$NODES
       export EUDICO_PATH=$NODE_3_PATH
       ./eudico wait-api;
       ./eudico net listen | grep '/ip6/::1/' > $NODE_3_NETADDR; sleep 3;
       ./eudico net connect \$(cat $NODE_0_NETADDR);
       ./eudico wallet import --as-default $NODE_3_KEY
       ./eudico mir miner --default-key 2>&1  | tee $NODE_3_MINER_LOG" Enter \; \
  attach-session -t "mir:0.1"
