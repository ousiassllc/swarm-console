.PHONY: build vet lint fmt fmt-check linterly test check

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

# docs/ は仕様書（markdown）で、手書きソースの行数管理の対象外にする（docs/environment/setup.md の Linterly 節）。
linterly:
	linterly check --ignore 'docs/**'

test:
	go test -race ./...

# CI の ci.yml と同じ順序・同じコマンド。
check:
	$(MAKE) build
	$(MAKE) vet
	$(MAKE) lint
	$(MAKE) linterly
	$(MAKE) test
