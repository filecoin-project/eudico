# Eudico with Mir Consensus

This is an experimental code for internal research purposes. It shouldn't be used in production.

## Requirements
Eudico and [Mir](https://github.com/filecoin-project/mir) requirements must be satisfied.
The most important one is Go1.17+.

## Install

### Eudico
```
git clone git@github.com:filecoin-project/eudico.git
cd eudico
git submodule update --init --recursive
make eudico
```

## Run


### Mir in the root network
```
./scripts/mir/eud-mir-root.sh

```

### Tmux

To stop a demo running via tmux:
```
tmux kill-session -t mir
```

All other Tmux commands can be found in the [Tmux Cheat Sheet](https://tmuxcheatsheet.com/).

### Mir in subnet

To create a deployment run the following script:
```
./scripts/mir/eud-mir-subnet.sh
```

Then run the following commands:
```
 ./eudico subnet add --consensus 3 --name mir --validators 2
 ./eudico subnet join --subnet=/root/t01002 -val-addr=127.0.0.1:10000 10 
 ./eudico subnet mine  --subnet=/root/t01002
 ./eudico --subnet-api=/root/t01002 wallet list
 ./eudico subnet fund --from=X --subnet=/root/t01002 11
 ./eudico --subnet-api=/root/t01001 send <addr> 10
 ./eudico subnet list-subnets
 ./eudico subnet release --from=X --subnet=/root/t01002
```

Don't forget provide different addresses if you use Mir nodes on the same machine: 
```
./eudico subnet join --subnet=/root/t01002 -val-addr=127.0.0.1:10001 10 
```

`t01001` name is just an example, in your setup the real subnet name may be different.
