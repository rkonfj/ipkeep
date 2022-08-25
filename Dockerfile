FROM golang:alpine3.16 as builder
ONBUILD ARG GOPROXY
WORKDIR /build
ADD . /build/
ENV GOPROXY https://goproxy.cn
RUN go build -ldflags "-s -w"

FROM alpine:3.16
COPY --from=builder /build/ipkeep /bin/ipkeep
