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

HOST="$2"
HOST=${HOST:-127.0.0.1}

# Computed vars
ENDPORT=$((PORT+NODES))

if [ "$1" == "start" ]
then
    while [ $((PORT < ENDPORT)) != "0" ]; do
        PORT=$((PORT+1))
        echo "Starting $PORT"
        ${REDISDIR}/redis-server --bind ${HOST} --port $PORT --cluster-enabled yes --cluster-config-file nodes-${PORT}.conf --cluster-node-timeout $TIMEOUT --appendonly yes --appendfilename appendonly-${PORT}.aof --dbfilename dump-${PORT}.rdb --logfile ${PORT}.log --daemonize yes
    done
    exit 0
fi

if [ "$1" == "create" ]
then
    HOSTS=""
    while [ $((PORT < ENDPORT)) != "0" ]; do
        PORT=$((PORT+1))
        HOSTS="$HOSTS $HOST:$PORT"
    done
    ${REDISDIR}/redis-cli --cluster create $HOSTS --cluster-replicas $REPLICAS
    exit 0
fi

if [ "$1" == "stop" ]
then
    while [ $((PORT < ENDPORT)) != "0" ]; do
        PORT=$((PORT+1))
        echo "Stopping $PORT"
        ${REDISDIR}/redis-cli -h ${HOST} -p $PORT shutdown nosave
    done
    exit 0
fi

if [ "$1" == "watch" ]
then
    PORT=$((PORT+1))
    while [ 1 ]; do
        clear
        date
        ${REDISDIR}/redis-cli -h ${HOST} -p $PORT cluster nodes | head -30
        sleep 1
    done
    exit 0
fi

if [ "$1" == "tail" ]
then
    INSTANCE=$2
    PORT=$((PORT+INSTANCE))
    tail -f ${PORT}.log
    exit 0
fi

if [ "$1" == "call" ]
then
    while [ $((PORT < ENDPORT)) != "0" ]; do
        PORT=$((PORT+1))
        ${REDISDIR}/redis-cli -h ${HOST} -p $PORT $2 $3 $4 $5 $6 $7 $8 $9
    done
    exit 0
fi

if [ "$1" == "clean" ]
then
    rm -rf *.log
    rm -rf appendonly*.aof
    rm -rf dump*.rdb
    rm -rf nodes*.conf
    exit 0
fi

if [ "$1" == "clean-logs" ]
then
    rm -rf *.log
    exit 0
fi

echo "Usage: $0 [start|create|stop|watch|tail|clean|clean-logs|call]"
echo "start [host]  -- Launch Redis Cluster instances (default host: 127.0.0.1)."
echo "create [host] -- Create a cluster using redis-cli --cluster create."
echo "stop [host]   -- Stop Redis Cluster instances."
echo "watch [host]  -- Show CLUSTER NODES output (first 30 lines) of first node."
echo "tail <id>     -- Run tail -f of instance at base port + ID."
echo "call <cmd>    -- Run a redis-cli command on all cluster nodes."
echo "clean         -- Remove all instances data, logs, configs."
echo "clean-logs    -- Remove just instances logs."
