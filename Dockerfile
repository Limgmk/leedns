FROM golang:1.16-alpine as builder
WORKDIR /usr/src/leedns
COPY . .
RUN CGO_ENABLED=0 go build -ldflags '-s -w --extldflags "-static -fpic"' -o target/leedns

FROM alpine as runner
COPY --from=builder /usr/src/leedns/target/leedns /usr/bin/leedns
COPY --from=builder /usr/src/leedns/config_example.yaml /etc/leedns/config.yaml

CMD ["/usr/bin/leedns", "--config", "/etc/leedns/config.yaml"]
