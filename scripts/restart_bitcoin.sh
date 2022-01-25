docker stop bitcoind_regtest
docker rm bitcoind_regtest
docker run -d --network=host --mount type=bind,source=${HOME}/bitcoin.conf,target=/root/.bitcoin/bitcoin.conf --name bitcoind_regtest rllola/bitcoind-fil-demo