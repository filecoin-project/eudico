rm -rf ./eudico
make eudico
rm -rvf ~/.eudico
rm -rvf ~/.tendermint/config
rm -rvf ~/.tendermint/data

tendermint init validator --key=secp256k1
#mkdir ~/.tendermint/data
#cp ~/.tendermint/priv_validator_state.json ~/.tendermint/data
#cp ~/.tendermint/genesis.json ~/.tendermint/config

sleep 2;

#./eudico wallet import--as-default /Users/alpha/./testdata/f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy.key; sleep 2;

tmux new-session -d -s "tendermint" \; \
  split-window -t "tendermint:0" -h \; \
  split-window -t "tendermint:0.0" -v \; \
  split-window -t "tendermint:0.2" -v \; \
  send-keys -t "tendermint:0.0" "tendermint start" Enter \; \
  send-keys -t "tendermint:0.1" "./eudico tendermint application" Enter \; \
  send-keys -t "tendermint:0.2" "sleep 6;
      ./eudico tendermint daemon --genesis=./testdata/tendermint.gen" Enter \; \
  send-keys -t "tendermint:0.3" "./eudico wait-api;
      ./eudico wallet import-tendermint-key --as-default -path=/Users/alpha/.tendermint/config/priv_validator_key.json; sleep 2;
      ./eudico tendermint miner --default-key" Enter \; \
  attach-session -t "tendermint:0.1"
