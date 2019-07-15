FROM golang

WORKDIR /

COPY golang/examples/main.go .

RUN CGO_ENABLED=0 GOOS=linux go build main.go

FROM scratch

COPY --from=0 /main /

EXPOSE 8080

CMD ["/main"]
