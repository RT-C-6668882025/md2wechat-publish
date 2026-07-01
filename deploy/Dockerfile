# syntax=docker/dockerfile:1

FROM golang:1.26.1-alpine3.23 AS build

ARG VERSION=dev
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/md2wechat-publish .

FROM alpine:3.22.5

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 publisher
COPY --from=build /out/md2wechat-publish /usr/local/bin/md2wechat-publish

USER publisher
WORKDIR /work
ENTRYPOINT ["/usr/local/bin/md2wechat-publish"]
