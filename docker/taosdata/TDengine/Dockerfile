FROM gcc:latest

RUN apt update && \
    apt-get install -y cmake build-essential && \
    git clone https://github.com/taosdata/TDengine.git && \
    cd TDengine && mkdir build && cd build && \
    cmake .. && cmake --build . && \
    make install || true && \
    rm -rf /TDengine && \
    sed -i  's/# charset/charset/' /etc/taos/taos.cfg && \
    sed -i  's/# dataDir/dataDir/' /etc/taos/taos.cfg && \
    sed -i  's/# logDir/logDir/' /etc/taos/taos.cfg && \
    sed -i 's/^\(dataDir\s*\/mnt\)/#\1/' /etc/taos/taos.cfg

EXPOSE 6020
EXPOSE 6030
EXPOSE 6035

CMD ["/usr/bin/taosd", "-c", "/etc/taos/"]
