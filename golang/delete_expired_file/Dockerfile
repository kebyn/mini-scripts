FROM golang:1.26-alpine

COPY . /opt

WORKDIR /opt

RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o delete_expired_file main.go

FROM scratch

COPY --from=0 /opt/delete_expired_file /delete_expired_file

ENTRYPOINT ["/delete_expired_file"]
CMD ["-p", "."]
