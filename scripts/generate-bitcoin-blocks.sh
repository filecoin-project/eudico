#! /bin/bash

while sleep 10; do curl -u satoshi:amiens -X POST \
    127.0.0.1:18443 \
    -d "{\"jsonrpc\":\"2.0\",\"id\":\"0\",\"method\":\"generatetoaddress\", \"params\": [1, \"bcrt1qgp62tlj8hwd7lpp0thz0ujjvgxsjug5hr4l8xj\"]}" \
    -H 'Content-Type:application/json'; done