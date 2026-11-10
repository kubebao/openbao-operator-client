.PHONY: install lint lint-fix test check build architecture tooling vulncheck licenses check-licenses check-modules consumer

GOLANGCI_LINT = go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
ACTIONLINT = go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

install:
	go mod download

lint:
	$(GOLANGCI_LINT) run ./...

lint-fix:
	$(GOLANGCI_LINT) fmt ./...
	$(GOLANGCI_LINT) run --fix ./...

test:
	go test -race ./...

build:
	go build ./...

architecture:
	python3 scripts/check-architecture.py

tooling:
	python3 -m unittest discover -s scripts/tests -v
	$(ACTIONLINT)

check: lint test build architecture tooling

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

licenses:
	bash scripts/third-party-licenses.sh

check-licenses:
	@notice=$$(mktemp) && trap 'rm -f "$$notice"' EXIT && \
		bash scripts/third-party-licenses.sh "$$notice" && diff -u THIRD_PARTY_LICENSES "$$notice"

check-modules:
	bash scripts/check-modules.sh

# REVISION must identify a commit already published in the public repository.
consumer:
	test -n "$(REVISION)"
	bash scripts/check-consumer.sh "$(REVISION)"
