# Mir Scripts

`eud-mir-root-grpc.sh` ran Mir in the rootnet with gRPC network transport. This script is for reference purposes only.

`connect.sh` - is a gadget to connect Eudico nodes with each other.

`eud-mir-root-libp2p.sh` runs Mir in the rootnet with libp2p network transport.

`eud-mir-subnet.sh` runs Mir in a subnet, the rootnet uses PoW.

`eud-3-mir-subnet.sh` runs Mir in a subnet of the rootnet running with 3 PoW miners.

`eud-4-mir-subnet.sh` runs Mir in a subnet of the rootnet running with 4 PoW miners.

## Scenario 1
In this scenario we use 3 Eudico nodes, we run PoW in the rootnet and Mir in a subnet.

To run the testbed use this script: `./scripts/mir/eud-3-mir-subnet.sh`.

Then you need to create a subnet. 

Run this on Eudico 0:

```
./eudico subnet add --consensus mir --name mir --min-validators 2
```

Let's suppose that the subnet is `/root/t01003`.

Two validators must join it to start mining.

Run this on Eudico 0:
```
./eudico subnet join --subnet=/root/t01003 -val-addr=/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ 10
./eudico subnet mine  --subnet=/root/t01003 --log-file=mir_miner_0.log --log-level=debug
```

Run this on Eudico 1:

```
./eudico subnet join --subnet=/root/t01003 -val-addr=/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWKPASbibHcHMCEuUk5qx4AQbbPiNgot7F4A4VPeEV6srp 10
./eudico subnet mine --subnet=/root/t01003 --log-file=mir_miner_1.log --log-level=debug
```

After that ISS will be started in the subnet.

Now let's send some money on the subnet.

Eudico 0:
```
./eudico subnet fund --from=t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy --subnet=/root/t01003 11
./eudico --subnet-api=/root/t01003 send t1k7t2zufxvtgamk7ogoifa5mvdagb4cafu6pdzga 1
```

Eudico 1:
```
./eudico --subnet-api=/root/t01003 wallet list

```
It should output the balance equal to 1 FIL.

Another Eudico node decided to join the subnet:

Eudico 2:
```
./eudico subnet join --subnet=/root/t01003 -val-addr=/ip4/127.0.0.1/tcp/10002/p2p/12D3KooWNuDQaGuwVLPyroJ4FZyNkiFcH2Qi61bNGehK2Mhgq3TK 10
./eudico subnet mine --subnet=/root/t01003 --log-file=mir_miner_1.log --log-level=debug
```

Now let's send some money on the subnet from this node.

Eudico 2:
```
./eudico subnet fund --from=t1rlhubezzmetmmpxyze22tc2uxuiiqv3iy6rvpra --subnet=/root/t01003 11
./eudico --subnet-api=/root/t01003 send t1k7t2zufxvtgamk7ogoifa5mvdagb4cafu6pdzga 3
```

Eudico 1:
```
./eudico --subnet-api=/root/t01003 wallet list

```

It should output the balance equal to 4 FIL.