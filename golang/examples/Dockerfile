FROM golang:1.22-alpine

WORKDIR /app

COPY main.go go.mod ./

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o /main main.go

FROM scratch

COPY --from=0 /main /main

EXPOSE 8080

CMD ["/main"]
