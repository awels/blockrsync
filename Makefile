# Copyright 2024 The blockrsync Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

IMAGE?=blockrsync
TAG?=latest
DOCKER_REPO?=quay.io/awels
GOOS?=linux
GOARCH?=amd64
BUILDAH_TLS_VERIFY?=true
BUILDAH_PLATFORM_FLAG?=--platform $(GOOS)/$(GOARCH)

export TAG
export GOOS
export GOARCH

all: test build

blockrsync:
	go build -o _out/blockrsync ./cmd/blockrsync/main.go

proxy:
	go build -o _out/proxy ./cmd/proxy/main.go

image: blockrsync proxy
	buildah build $(BUILDAH_PLATFORM_FLAG) -t $(DOCKER_REPO)/$(IMAGE):$(GOARCH) -f Dockerfile .

manifest: image
	-buildah manifest create $(DOCKER_REPO)/$(IMAGE):local
	buildah manifest add --arch $(GOARCH) $(DOCKER_REPO)/$(IMAGE):local containers-storage:$(DOCKER_REPO)/$(IMAGE):$(GOARCH)

push: manifest manifest-push

manifest-push:
	buildah manifest push --tls-verify=${BUILDAH_TLS_VERIFY} --all $(DOCKER_REPO)/$(IMAGE):local docker://$(DOCKER_REPO)/$(IMAGE):$(TAG)

manifest-clean:
	-buildah manifest rm $(DOCKER_REPO)/$(IMAGE):local

clean: manifest-clean
	GO111MODULE=on; \
	go mod tidy; \
	go mod vendor; \
	rm -rf _out

build: clean blockrsync

test:
	go test ./...
