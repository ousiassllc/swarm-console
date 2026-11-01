.PHONY: build vet lint fmt fmt-check linterly plan-test test check

build:
	go build -o bin/swarm-console ./cmd/swarm-console

vet:
	go vet ./...

lint:
	golangci-lint run ./...

fmt:
	golangci-lint fmt ./...

fmt-check:
	golangci-lint fmt --diff ./...

linterly:
	linterly check

plan-test:
	bash .github/scripts/plan-release_test.sh

test:
	go test -race ./...

# CI の ci.yml と同じ順序・同じコマンド。
check:
	$(MAKE) build
	$(MAKE) vet
	$(MAKE) lint
	$(MAKE) linterly
	$(MAKE) plan-test
	$(MAKE) test
