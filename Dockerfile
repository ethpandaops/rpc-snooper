# build env
FROM golang:1.21 AS build-env
COPY go.mod go.sum /src/
WORKDIR /src
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG release=
RUN <<EOR
  VERSION=$(git rev-parse --short HEAD)
  RELEASE=$release
  CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /app/snooper -ldflags="-s -w -X 'github.com/ethpandaops/rpc-snooper/utils.BuildVersion=${VERSION}' -X 'github.com/ethpandaops/rpc-snooper/utils.BuildRelease=${RELEASE}'" ./cmd/snooper
EOR

# final stage
FROM debian:stable-slim
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates
RUN update-ca-certificates
ENV PATH="$PATH:/app"
COPY --from=build-env /app/* /app
RUN ln -s /app/snooper /app/json_rpc_snoop
CMD ["./snooper", "--help"]
