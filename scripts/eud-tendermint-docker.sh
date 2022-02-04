rm -rf ./eudico
make eudico
rm -rvf ~/.eudico


sleep 2;

#./eudico wallet import--as-default /Users/alpha/./testdata/f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy.key; sleep 2;

tmux new-session -d -s "tendermint3" \; \
  split-window -t "tendermint3:0" -h \; \
  split-window -t "tendermint3:0.0" -v \; \
  split-window -t "tendermint3:0.2" -v \; \
  send-keys -t "tendermint3:0.0" "./eudico tendermint daemon --genesis=./testdata/gen.gen" Enter \; \
  send-keys -t "tendermint3:0.1" "./eudico wait-api;
        ./eudico wallet import-tendermint-key --as-default -path=/Users/alpha/Projects/tendermint/build/node0/config/priv_validator_key.json; sleep 2;
        ./eudico tendermint miner f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy" Enter \; \
  send-keys -t "tendermint3:0.1" "./eudico tendermint application" Enter \; \
  send-keys -t "tendermint3:0.2" "sleep 6;
      ./eudico tendermint daemon --genesis=./testdata/gen.gen" Enter \; \
  send-keys -t "tendermint3:0.3" "./eudico wait-api;
      ./eudico wallet import-tendermint-key --as-default -path=/Users/alpha/.tendermint/config/priv_validator_key.json; sleep 2;
      ./eudico tendermint miner f1ozbo7zqwfx6d4tqb353qoq7sfp4qhycefx6ftgy" Enter \; \
  attach-session -t "tendermint:0.1"