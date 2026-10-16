.PHONY: tools generate test vet build integration

PATH := $(shell go env GOPATH)/bin:$(PATH)

tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.6
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

generate:
	protoc -I proto --go_out=. --go_opt=module=github.com/orbit-projects/orbit-kubernetes-go --go-grpc_out=. --go-grpc_opt=module=github.com/orbit-projects/orbit-kubernetes-go proto/orbit/plugin/v1/process_plugin.proto
	go run ./scripts/add-generated-license.go

test:
	sh ./scripts/test-local-workspace.sh test

vet:
	sh ./scripts/test-local-workspace.sh vet

build:
	sh ./scripts/test-local-workspace.sh build

integration:
	sh ./scripts/test-local-workspace.sh integration
