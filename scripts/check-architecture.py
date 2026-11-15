#!/usr/bin/env python3
"""Enforce the library's package import boundaries, including test imports."""

import json
from pathlib import Path
import subprocess
import sys

MODULE = "github.com/kubebao/openbao-operator-client"
LAYERS = {
    "evidence": set(),
    "adapter": {"evidence"},
    "collect": {"adapter", "evidence", "internal/apiendpoint"},
    "diagnose": {"evidence"},
    "operations": {"adapter", "collect", "evidence", "internal/apiendpoint"},
    "internal/apiendpoint": set(),
}
FORBIDDEN = (
    "github.com/kubebao/openbao-operator",
    "github.com/kubebao/openbao-operator-console",
    "github.com/kubebao/openbao-operator-cli",
    "github.com/dc-tec/openbao-operator",
)


def forbidden(path):
    return any(path == prefix or path.startswith(prefix + "/") for prefix in FORBIDDEN)


def layer_of(path):
    """Return the layer for a module-relative package path; internal packages are layers of their own."""
    parts = path.split("/")
    if parts[0] == "internal" and len(parts) > 1:
        return "/".join(parts[:2])
    return parts[0]


def violations(package):
    """Return boundary violations for one go-list package description."""
    path = package["ImportPath"].split(" ", 1)[0].removesuffix("_test")
    if not path.startswith(MODULE + "/"):
        return []
    layer = layer_of(path.removeprefix(MODULE + "/"))
    imports = package.get("Imports", []) + package.get("TestImports", []) + package.get("XTestImports", [])
    errors = []
    for imported in imports:
        imported = imported.split(" ", 1)[0].removesuffix("_test")
        if forbidden(imported):
            errors.append(f"{path}: consumer/operator dependency {imported}")
        if imported.startswith(MODULE + "/"):
            target = layer_of(imported.removeprefix(MODULE + "/"))
            if layer in LAYERS and target != layer and target not in LAYERS[layer]:
                errors.append(f"{path}: disallowed package direction to {imported}")
        elif layer in {"evidence", "diagnose"}:
            if "." in imported.split("/", 1)[0]:
                errors.append(f"{path}: external dependency in pure package: {imported}")
            if imported == "net" or imported.startswith("net/") or imported == "os/exec" or imported.startswith("database/"):
                errors.append(f"{path}: effectful dependency in pure package: {imported}")
    if layer in {"evidence", "diagnose"}:
        for dependency in package.get("Deps", []):
            if dependency.startswith(("k8s.io/", "sigs.k8s.io/")) or forbidden(dependency):
                errors.append(f"{path}: forbidden transitive dependency {dependency}")
    return errors


def main():
    root = Path(__file__).resolve().parents[1]
    result = subprocess.run(["go", "list", "-json", "./..."], cwd=root, text=True, capture_output=True, check=True)
    remaining = result.stdout.lstrip()
    decoder = json.JSONDecoder()
    errors = []
    while remaining:
        package, offset = decoder.raw_decode(remaining)
        errors.extend(violations(package))
        remaining = remaining[offset:].lstrip()
    if errors:
        print("\n".join(sorted(set(errors))), file=sys.stderr)
        return 1
    print("Package boundaries passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
