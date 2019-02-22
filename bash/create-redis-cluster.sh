#!/bin/bash

# Settings
# 7001,7002,7003,7004,7005,7006
PORT=7000
TIMEOUT=2000
# CLUSTER NODES
NODES=6
REPLICAS=1
# Redis Command Directory
REDISDIR="../../src/"

# You may want to put the above config parameters into config.sh in order to
# override the defaults without modifying this script.

if [ -a config.sh ]
then
    source "config.sh"
fi

HOST=""
HOST=127.0.0.1

# Computed vars
ENDPORT=0

if [ "" == "start" ]
then
    while [ 0 != "0" ]; do
        PORT=1
        echo "Starting "
        /redis-server --bind  --port  --cluster-enabled yes --cluster-config-file nodes-.conf --cluster-node-timeout  --appendonly yes --appendfilename appendonly-.aof --dbfilename dump-.rdb --logfile .log --daemonize yes
    done
    exit 0
fi

if [ "" == "create" ]
then
    HOSTS=""
    while [ 0 != "0" ]; do
        PORT=1
        HOSTS=" :"
    done
    /redis-cli --cluster create  --cluster-replicas 
    exit 0
fi

if [ "" == "stop" ]
then
    while [ 0 != "0" ]; do
        PORT=1
        echo "Stopping "
        /redis-cli -h  -p  shutdown nosave
    done
    exit 0
fi

if [ "" == "watch" ]
then
    PORT=1
    while [ 1 ]; do
        clear
        date
        /redis-cli -h  -p  cluster nodes | head -30
        sleep 1
    done
    exit 0
fi

if [ "" == "tail" ]
then
    INSTANCE=
    PORT=0
    tail -f .log
    exit 0
fi

if [ "" == "call" ]
then
    while [ 0 != "0" ]; do
        PORT=1
        /redis-cli -h  -p         
    done
    exit 0
fi

if [ "" == "clean" ]
then
    rm -rf *.log
    rm -rf appendonly*.aof
    rm -rf dump*.rdb
    rm -rf nodes*.conf
    exit 0
fi

if [ "" == "clean-logs" ]
then
    rm -rf *.log
    exit 0
fi

echo "Usage: -bash [start|create|stop|watch|tail|clean]"
echo "start       -- Launch Redis Cluster instances."
echo "create      -- Create a cluster using redis-cli --cluster create."
echo "stop        -- Stop Redis Cluster instances."
echo "watch       -- Show CLUSTER NODES output (first 30 lines) of first node."
echo "tail <id>   -- Run tail -f of instance at base port + ID."
echo "clean       -- Remove all instances data, logs, configs."
echo "clean-logs  -- Remove just instances logs."
