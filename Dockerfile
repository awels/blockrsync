######################################################################
# Establish a common builder image for all golang-based images
FROM docker.io/golang:1.21 as golang-builder
USER root
WORKDIR /workspace
# We don't vendor modules. Enforce that behavior
#ENV GOFLAGS=-mod=readonly
#ENV GO111MODULE=on
ARG TARGETOS
ARG TARGETARCH
ENV GOOS=${TARGETOS:-linux}
ENV GOARCH=${TARGETARCH}

######################################################################
# Build block binary
FROM golang-builder AS blockrsync-builder

# Copy the Go Modules manifests & download dependencies
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

# Copy the go source
COPY cmd/ cmd/
COPY ./vendor/. vendor/
COPY ./pkg/. pkg/

# Build
RUN go build -o blockrsync ./cmd/blockrsync/main.go
RUN go build -o proxy ./cmd/proxy/main.go

######################################################################
# Final container

FROM registry.access.redhat.com/ubi9-minimal
WORKDIR /

##### blockrsync
COPY --from=blockrsync-builder /workspace/blockrsync /blockrsync
COPY --from=blockrsync-builder /workspace/proxy /proxy

##### Set build metadata
ARG builddate_arg="(unknown)"
ARG version_arg="(unknown)"
ENV builddate="${builddate_arg}"
ENV version="${version_arg}"

# https://github.com/opencontainers/image-spec/blob/main/annotations.md
LABEL org.opencontainers.image.base.name="registry.access.redhat.com/ubi9-minimal"
LABEL org.opencontainers.image.created="${builddate}"
LABEL org.opencontainers.image.description="blockrsync is a tool to synchronize block devices over the network."
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.revision="${version}"
LABEL org.opencontainers.image.source="https://github.com/awels/blockrsync"
LABEL org.opencontainers.image.title="blockrsync"
LABEL org.opencontainers.image.version="${version}"

ENTRYPOINT [ "/bin/bash" ]
