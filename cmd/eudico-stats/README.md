# eudico-stats

`eudico-stats` is a small tool to push chain information into influxdb

## Usage

eudico-stats will look in `~/.eudico` to connect to a running daemon and resume collecting stats from last record block height.

For other usage see `./eudico-stats --help`

```
# step 1. Build the source code
make eudico-stats

# step 2. start your node and miner

# step 3. export the FULL NODE API env variable
./eudico auth api-info --perm admin

# step 4. up influxdb and grafana
cd cmd/eudico-stats && docker-compose up -d && cd -

# step 5. launch eudico stats
./eudico-stats run --no-sync true

# you should be able to see the stats on http://localhost:9090 with eudico*
```
