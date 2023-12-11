FROM golang:1-alpine AS builder

COPY . /go/src/github.com/cedar2025/cloudflareddns
WORKDIR /go/src/github.com/cedar2025/cloudflareddns

RUN go build -v -o CloudflareDDNS -trimpath -ldflags "-s -w -buildid="

FROM alpine
COPY --from=builder /go/src/github.com/cedar2025/cloudflareddns/CloudflareDDNS /usr/local/bin/CloudflareDDNS

WORKDIR /CloudflareDDNS

CMD ["/usr/local/bin/CloudflareDDNS"]