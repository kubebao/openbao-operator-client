#!/usr/bin/env python3
"""Build a public module consumer with fresh caches and automatic temporary cleanup."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

MODULE = "github.com/kubebao/openbao-operator-client"
SOURCE = '''package main

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
'''


def main():
    root = Path(__file__).resolve().parents[1]
    goroot = subprocess.run(["go", "env", "GOROOT"], cwd=root, text=True, capture_output=True, check=True).stdout.strip()
    executable = str(Path(goroot) / "bin/go")
    with tempfile.TemporaryDirectory(prefix="openbao-client-consumer-") as directory:
        temporary = Path(directory)
        workspace = temporary / "module"
        workspace.mkdir()
        environment = dict(os.environ)
        for key in list(environment):
            if key.startswith(("GH_", "GITHUB_", "GIT_", "SSH_")):
                environment.pop(key)
        environment.update({
            "GOWORK": "off", "GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": "", "GOAUTH": "off",
            "GOMODCACHE": str(temporary / "cache"), "GOCACHE": str(temporary / "build-cache"),
            "GOTOOLCHAIN": "local", "GOFLAGS": "-modcacherw", "GOPROXY": "https://proxy.golang.org",
            "GOSUMDB": "sum.golang.org", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_TERMINAL_PROMPT": "0",
        })

        def run(*arguments):
            return subprocess.run([executable, *arguments], cwd=workspace, env=environment, text=True,
                                  capture_output=True, check=True).stdout.strip()

        run("mod", "init", "example.com/client-consumer")
        run("mod", "edit", "-go=1.27.3")
        (workspace / "main.go").write_text(SOURCE)
        run("get", MODULE + "@" + sys.argv[1])
        run("mod", "tidy")
        run("mod", "verify")
        if run("run", "-mod=readonly", ".") != "stale":
            raise RuntimeError("External consumer returned an unexpected generation result")
        module = json.loads(run("list", "-m", "-json", MODULE))
        if "Replace" in module:
            raise RuntimeError("External consumer unexpectedly uses a replacement")
        print(f"Unauthenticated external consumer passed: {module['Path']} {module['Version']}")


if __name__ == "__main__":
    try:
        main()
    except subprocess.CalledProcessError as error:
        print(error.stderr, file=sys.stderr)
        sys.exit(1)
