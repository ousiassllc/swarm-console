.PHONY: build vet lint fmt fmt-check linterly plan-test install-test test check install

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

install-test:
	bash scripts/install_test.sh

# 最新リリースを取って SHA256SUMS で照合し、~/bin/swarm-console に置き、systemd のユーザーサービスとして常駐させる（docs/requirements/functional-install.md）。
install:
	bash scripts/install.sh

test:
	go test -race ./...

# CI の ci.yml と同じ順序・同じコマンド。
check:
	$(MAKE) build
	$(MAKE) vet
	$(MAKE) lint
	$(MAKE) linterly
	$(MAKE) plan-test
	$(MAKE) install-test
	$(MAKE) test
