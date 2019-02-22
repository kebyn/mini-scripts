### 1. redis github
  - [redis](https://github.com/antirez/redis)

### 2. cluster tutorial
  - [cluster-tutorial](https://redis.io/topics/cluster-tutorial)

### 3. redis cluster
  - [create-cluster](https://github.com/antirez/redis/tree/unstable/utils/create-cluster)
  ```
  Creating a Redis Cluster using the create-cluster script
  If you don't want to create a Redis Cluster by configuring and executing individual instances manually as explained above, there is a much simpler system (but you'll not learn the same amount of operational details).

  Just check utils/create-cluster directory in the Redis distribution. There is a script called create-cluster inside (same name as the directory it is contained into), it's a simple bash script. In order to start a 6 nodes cluster with 3 masters and 3 slaves just type the following commands:
  ```
    `create-cluster start`
    `create-cluster create`
  ```
  Reply to yes in step 2 when the redis-cli utility wants you to accept the cluster layout.

  You can now interact with the cluster, the first node will start at port 30001 by default. When you are done, stop the cluster with:
  ```
    `create-cluster stop`
  ```
  Please read the README inside this directory for more information on how to run the script.
  ```

### 4. redis cluster listen 10.10.10.10
  - redis cluster default listen 127.0.0.1
  ```
  create-redis-cluster.sh start 10.10.10.10
  create-redis-cluster.sh create 10.10.10.10
  create-redis-cluster.sh stop 10.10.10.10
  ```
### 5. other setting
  ```
  # Settings
  # 7001,7002,7003,7004,7005,7006
  PORT=7000
  TIMEOUT=2000
  # CLUSTER NODES
  NODES=6
  REPLICAS=1
  # Redis Command Directory
  REDISDIR="../../src/"
```
