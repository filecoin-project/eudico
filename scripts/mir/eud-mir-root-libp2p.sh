#!/usr/bin/env bash

# Mir IDs
NODE_0=/root:t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy
NODE_1=/root:t1k7t2zufxvtgamk7ogoifa5mvdagb4cafu6pdzga
NODE_2=/root:t1rlhubezzmetmmpxyze22tc2uxuiiqv3iy6rvpra
NODE_3=/root:t1sqbkluz5elnekdu62ute5zjammslkplgdcpa2zi

# Libp2p IDs
NODE_0_LIBP2P=12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ
NODE_1_LIBP2P=12D3KooWKPASbibHcHMCEuUk5qx4AQbbPiNgot7F4A4VPeEV6srp
NODE_2_LIBP2P=12D3KooWNuDQaGuwVLPyroJ4FZyNkiFcH2Qi61bNGehK2Mhgq3TK
NODE_3_LIBP2P=12D3KooWRF48VL58mRkp5DmysHp2wLwWyycJ6df2ocEHPvRxMrLs

# Persistent nodes in Tendermint-type format
NODES=\
/root:t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ,\
/root:t1k7t2zufxvtgamk7ogoifa5mvdagb4cafu6pdzga@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWKPASbibHcHMCEuUk5qx4AQbbPiNgot7F4A4VPeEV6srp,\
/root:t1rlhubezzmetmmpxyze22tc2uxuiiqv3iy6rvpra@/ip4/127.0.0.1/tcp/10002/p2p/12D3KooWNuDQaGuwVLPyroJ4FZyNkiFcH2Qi61bNGehK2Mhgq3TK,\
/root:t1sqbkluz5elnekdu62ute5zjammslkplgdcpa2zi@/ip4/127.0.0.1/tcp/10003/p2p/12D3KooWRF48VL58mRkp5DmysHp2wLwWyycJ6df2ocEHPvRxMrLs

# Eudico paths
export NODE_0_PATH="$HOME/.eudico-node0"
export NODE_1_PATH="$HOME/.eudico-node1"
export NODE_2_PATH="$HOME/.eudico-node2"
export NODE_3_PATH="$HOME/.eudico-node3"

WALLET_0_KEY="./testdata/wallet/node0.key"
WALLET_1_KEY="./testdata/wallet/node1.key"
WALLET_2_KEY="./testdata/wallet/node2.key"
WALLET_3_KEY="./testdata/wallet/node3.key"

NODE_0_KEY="./testdata/libp2p/node0.keyinfo"
NODE_1_KEY="./testdata/libp2p/node1.keyinfo"
NODE_2_KEY="./testdata/libp2p/node2.keyinfo"
NODE_3_KEY="./testdata/libp2p/node3.keyinfo"

export NODE_0_NETADDR="$NODE_0_PATH/.netaddr"
export NODE_1_NETADDR="$NODE_1_PATH/.netaddr"
export NODE_2_NETADDR="$NODE_2_PATH/.netaddr"
export NODE_3_NETADDR="$NODE_3_PATH/.netaddr"

# Eudico API ports
NODE_0_API="1234"
NODE_1_API="1235"
NODE_2_API="1236"
NODE_3_API="1237"

SHED_0_LOG="./eudico_shed_0.log"
SHED_1_LOG="./eudico_shed_1.log"
SHED_2_LOG="./eudico_shed_2.log"
SHED_3_LOG="./eudico_shed_3.log"
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
rm -rf ./eudico-wal*
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
rm -rf ./eudico_shed_*.log

LOG_LEVEL="info,mir-consensus=debug,mir-manager=debug"

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
        export GOLOG_LOG_LEVEL=$LOG_LEVEL EUDICO_PATH=$NODE_0_PATH LOTUS_PATH=$NODE_0_PATH
        mkdir -p $NODE_0_PATH/keystore && chmod 0700 $NODE_0_PATH/keystore;
        ./lotus-shed keyinfo import $NODE_0_KEY;
        ./eudico mir daemon --genesis=$BLOCK0 --api=$NODE_0_API 2>&1 | tee $NODE_0_DAEMON_LOG" Enter \; \
  send-keys -t "mir:0.1" "
        export EUDICO_MIR_VALIDATORS=$NODES EUDICO_PATH=$NODE_0_PATH GOLOG_LOG_LEVEL=$LOG_LEVEL
        ./eudico wait-api;
        source ./scripts/mir/connect.sh 0;
        ./eudico wallet import --as-default $WALLET_0_KEY;
        ./eudico mir miner --default-key 2>&1 | tee $NODE_0_MINER_LOG" Enter \; \
  send-keys -t "mir:0.2" "
        export GOLOG_LOG_LEVEL=$LOG_LEVEL EUDICO_PATH=$NODE_1_PATH LOTUS_PATH=$NODE_1_PATH
        mkdir -p $NODE_1_PATH/keystore && chmod 0700 $NODE_1_PATH/keystore;
        ./lotus-shed keyinfo import $NODE_1_KEY;
        ./eudico mir daemon --genesis=$BLOCK0 --api=$NODE_1_API 2>&1 | tee $NODE_1_DAEMON_LOG" Enter \; \
  send-keys -t "mir:0.3" "
        export GOLOG_LOG_LEVEL=$LOG_LEVEL EUDICO_MIR_VALIDATORS=$NODES EUDICO_PATH=$NODE_1_PATH
        ./eudico wait-api;
        source ./scripts/mir/connect.sh 1;
        ./eudico wallet import --as-default $WALLET_1_KEY;
        ./eudico mir miner --default-key 2>&1 | tee $NODE_1_MINER_LOG" Enter \; \
  \
  send-keys -t "mir:1.0" "
        export GOLOG_LOG_LEVEL=$LOG_LEVEL EUDICO_PATH=$NODE_2_PATH LOTUS_PATH=$NODE_2_PATH
        mkdir -p $NODE_2_PATH/keystore && chmod 0700 $NODE_2_PATH/keystore;
        ./lotus-shed keyinfo import $NODE_2_KEY;
        ./eudico mir daemon --genesis=$BLOCK0 --api=$NODE_2_API 2>&1 | tee $NODE_2_DAEMON_LOG" Enter \; \
    send-keys -t "mir:1.1" "
        export GOLOG_LOG_LEVEL=$LOG_LEVEL EUDICO_MIR_VALIDATORS=$NODES EUDICO_PATH=$NODE_2_PATH
        ./eudico wait-api;
        source ./scripts/mir/connect.sh 2;
        ./eudico wallet import --as-default $WALLET_2_KEY;
        ./eudico mir miner --default-key 2>&1 | tee $NODE_2_MINER_LOG" Enter \; \
    send-keys -t "mir:1.2" "
        export GOLOG_LOG_LEVEL=$LOG_LEVEL EUDICO_PATH=$NODE_3_PATH LOTUS_PATH=$NODE_3_PATH
        mkdir -p $NODE_3_PATH/keystore && chmod 0700 $NODE_3_PATH/keystore;
        ./lotus-shed keyinfo import $NODE_3_KEY;
        ./eudico mir daemon --genesis=$BLOCK0 --api=$NODE_3_API 2>&1 | tee $NODE_3_DAEMON_LOG" Enter \; \
    send-keys -t "mir:1.3" "
       export GOLOG_LOG_LEVEL=$LOG_LEVEL EUDICO_MIR_VALIDATORS=$NODES EUDICO_PATH=$NODE_3_PATH
       ./eudico wait-api;
       source ./scripts/mir/connect.sh 3;
       ./eudico wallet import --as-default $WALLET_3_KEY
       ./eudico mir miner --default-key 2>&1  | tee $NODE_3_MINER_LOG" Enter \; \
  attach-session -t "mir:1.0"
