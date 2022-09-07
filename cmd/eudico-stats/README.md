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

# step 6. Add subnet
```
# e.g. ./eudico wallet set-default f1lubprfrtjvwodaszrtxow36p53cjx3nevdkboby
./eudico wallet set-default <Your Key>

./eudico subnet add --name test1 --consensus POW
./eudico subnet join --subnet /root/t01000 4

# fund subnets
./eudico subnet fund --subnet /root/t01000 100

# mine the created subnet
./eudico subnet mine --subnet /root/t01000

# create subnets in subnet and join
./eudico subnet add --name test2 --parent /root/t01000 --consensus POW
./eudico subnet join --subnet /root/t01000/t01001 4

# you should be able to see the stats on http://localhost:9090 with eudico*
```

Using docker-compose
In the root directory
```shell
docker build -t eudico:latest -f Dockerfile.eudico .
docker run -it -v `pwd`/credentials:/home/eudico/credentials --rm eudico:latest -i
docker-compose -f docker-compose.eudico.yaml up -d
docker-compose -f docker-compose.eudico.yaml logs -f all-in-one
docker-compose -f docker-compose.eudico.yaml exec all-in-one bash
docker-compose -f docker-compose.eudico.yaml scale all-in-one=5
```