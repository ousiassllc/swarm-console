# golangci-lint のキャッシュは、中身が同じパッケージを worktree をまたいで共有し、指摘の位置を別 worktree の絶対パスのまま返す（#32）。
# worktree ごとの置き場に分け、自分のファイルだけを対象にする（docs/environment/setup-quality.md）。
export GOLANGCI_LINT_CACHE := $(CURDIR)/.cache/golangci-lint

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

# CI の ci.yml と同じ順序・同じコマンド。先頭は CI の Build ステップと同じ go build ./...（全パッケージのコンパイル確認。bin は作らないので make build とは別）。
check:
	go build ./...
	$(MAKE) vet
	$(MAKE) lint
	$(MAKE) linterly
	$(MAKE) plan-test
	$(MAKE) install-test
	$(MAKE) test
