FROM golang:1.15

COPY . /opt

WORKDIR /opt

RUN CGO_ENABLED=0 go build  -ldflags "-s"

FROM scratch

COPY --from=0 /opt/delete_expired_file /delete_expired_file

ENTRYPOINT ["/delete_expired_file"]
CMD ["-p", "."]