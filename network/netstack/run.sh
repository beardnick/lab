#!/usr/bin/env bash

GOOS=linux go build -v -o gotcp

EXT=$?
if [[ $EXT -ne 0 ]]; then
    exit $EXT 
fi

sudo ./gotcp &
PID=$!
sudo ip addr add 192.168.0.1/24 dev mytun
sudo ip link set up dev mytun
trap "sudo killall gotcp" INT TERM
wait $PID 
