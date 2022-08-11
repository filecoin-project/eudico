# Testdata
This directory contains different data, identeties, keys and, genesis files and keys for testing, demo and debugging purposes.

## Info

1. Libp2p keys are stored in `testdata/libp2p`.
2. Wallet keys are stored in `testdata/wallet`.

## Commands

### Generating libp2p keys
This will create a file called libp2p-host-<address>.keyinfo which will contain a keyinfo json object:
```
lotus-shed keyinfo new libp2p-host
```

### Importing keys to lotus repository
Be sure to set the LOTUS_PATH before running the command if using a non-standard location,
if the directory does not exists, you will also need to create a keystore folder with proper permissions.
```
mkdir -p $LOTUS_PATH/keystore && chmod 0700 $LOTUS_PATH/keystore
```

```
lotus-shed keyinfo import <filename>.keyinfo
```

To use it with Eudico API:
```
LOTUS_PATH=$NODE_API_PATH
EUDICO_PATH=$NODE_API_PATH
```

## Identities

### Wallet

#### node0.key
t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy

#### node1.key
t1k7t2zufxvtgamk7ogoifa5mvdagb4cafu6pdzga

#### node2.key
t1rlhubezzmetmmpxyze22tc2uxuiiqv3iy6rvpra

#### node3.key
t1sqbkluz5elnekdu62ute5zjammslkplgdcpa2zi

#### node4.key
t1wmaksrs27k5j53aabwy6dianwgxqjtjiquq44fi


### Libp2p

#### node0.keyinfo

12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ

#### node1.keyinfo

12D3KooWKPASbibHcHMCEuUk5qx4AQbbPiNgot7F4A4VPeEV6srp

#### node2.keyinfo

12D3KooWNuDQaGuwVLPyroJ4FZyNkiFcH2Qi61bNGehK2Mhgq3TK

#### node3.keyinfo

12D3KooWRF48VL58mRkp5DmysHp2wLwWyycJ6df2ocEHPvRxMrLs

#### node4.keyinfo

12D3KooWRUDXegwwY6FLgqKuMEnGJSJ7XoMgHh7sE492fcXyDUGC

### Mir IDs
NODE_0=/root:t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy

NODE_1=/root:t1k7t2zufxvtgamk7ogoifa5mvdagb4cafu6pdzga

NODE_2=/root:t1rlhubezzmetmmpxyze22tc2uxuiiqv3iy6rvpra

NODE_3=/root:t1sqbkluz5elnekdu62ute5zjammslkplgdcpa2zi

NODE_4=/root:t1wmaksrs27k5j53aabwy6dianwgxqjtjiquq44fi

### Libp2p IDs
NODE_0_LIBP2P=12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ

NODE_1_LIBP2P=12D3KooWKPASbibHcHMCEuUk5qx4AQbbPiNgot7F4A4VPeEV6srp

NODE_2_LIBP2P=12D3KooWNuDQaGuwVLPyroJ4FZyNkiFcH2Qi61bNGehK2Mhgq3TK

NODE_3_LIBP2P=12D3KooWRF48VL58mRkp5DmysHp2wLwWyycJ6df2ocEHPvRxMrLs

NODE_4_LIBP2P=12D3KooWRUDXegwwY6FLgqKuMEnGJSJ7XoMgHh7sE492fcXyDUGC

NODE_5_LIBP2P=12D3KooWRcrEceg54ZJiHPpxiR8VQpjaiXBnb8P8C5jKzGinbiqU

NODE_6_LIBP2P=12D3KooWQ3uGUkYWz39GF9jcX7QUvfWQvx6EzdCFKt3Fx6gjm2ns

NODE_7_LIBP2P=12D3KooWHpQL2xwptmwfyun4uUffC8seKJdAhrhrvydfmW5U9m2Y

### Persistent nodes in Tendermint-style format
```
/root:t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ,
/root:t1k7t2zufxvtgamk7ogoifa5mvdagb4cafu6pdzga@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWKPASbibHcHMCEuUk5qx4AQbbPiNgot7F4A4VPeEV6srp,
/root:t1rlhubezzmetmmpxyze22tc2uxuiiqv3iy6rvpra@/ip4/127.0.0.1/tcp/10002/p2p/12D3KooWNuDQaGuwVLPyroJ4FZyNkiFcH2Qi61bNGehK2Mhgq3TK,
/root:t1sqbkluz5elnekdu62ute5zjammslkplgdcpa2zi@/ip4/127.0.0.1/tcp/10003/p2p/12D3KooWRF48VL58mRkp5DmysHp2wLwWyycJ6df2ocEHPvRxMrLs
```

