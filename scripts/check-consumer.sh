#!/usr/bin/env bash
# Fetch and compile a published commit without credentials or local replacements.
set -euo pipefail
revision=${1:?provide a published revision}
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/module" "$scratch/cache" "$scratch/build-cache"
export GOWORK=off GOPRIVATE= GONOPROXY= GONOSUMDB= GOAUTH=off
export GOMODCACHE="$scratch/cache" GOCACHE="$scratch/build-cache"
export GOPROXY=https://proxy.golang.org,direct GOSUMDB=sum.golang.org
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_TERMINAL_PROMPT=0
unset GH_TOKEN GITHUB_TOKEN GIT_CONFIG_COUNT GIT_CONFIG_PARAMETERS SSH_AUTH_SOCK
cd "$scratch/module"
go mod init example.com/client-consumer
go mod edit -go=1.27.3
cat > main.go <<'EOF'
package main

import (
    "fmt"
    _ "github.com/kubebao/openbao-operator-client/adapter"
    _ "github.com/kubebao/openbao-operator-client/collect"
    _ "github.com/kubebao/openbao-operator-client/diagnose"
    "github.com/kubebao/openbao-operator-client/evidence"
)

func main() {
    generation, observed := int64(3), int64(2)
    fmt.Println(evidence.CompareGeneration(&generation, &observed))
}
EOF
go get "github.com/kubebao/openbao-operator-client@$revision"
go mod tidy
go mod verify
test "$(go run -mod=readonly .)" = stale
go list -m -json github.com/kubebao/openbao-operator-client
