## docker/taosdata/TDengine
### Quickstart
```
docker run --name tdengine --interactive --rm -d kebyn/tdengine
```
#### use the TDengine shell to connect the TDengine server
```
docker exec -it tdengine bash
root@xxx:/# taos

Welcome to the TDengine shell, server version:1.6.1.0  client version:1.6.1.0
Copyright (c) 2017 by TAOS Data, Inc. All rights reserved.

taos>
```

## docker/thegreatjavascript/FakeScreenshot
### Quickstart
```
docker run --name fakescreenshot --publish 8000:8000 --rm -d kebyn/fakescreenshot
```
#### Open http://ip:8000 using a browser
>>> Must use ip address, domain name cannot be used

