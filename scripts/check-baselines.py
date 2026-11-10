#!/usr/bin/env python3
"""Run historical consumer tests in temporary archives of the recorded commits."""

import argparse
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile


ROOT = Path(__file__).resolve().parents[1]
BASELINES = {
    "console": ("9e7b6900f781e8fbac736ca42e6a51f7f807087b", ["./internal/collector", "./internal/evidence"]),
    "cli": ("adbe0f42fac62bdc6dd2d7995a8aecb36102e9a3", ["./internal/collect", "./internal/doctor", "./internal/evidence"]),
}


def run_baseline(name, source):
    revision, packages = BASELINES[name]
    actual = subprocess.check_output(
        ["git", "-C", str(source), "rev-parse", f"{revision}^{{commit}}"], text=True
    ).strip()
    if actual != revision:
        raise ValueError(f"{name}: baseline revision mismatch")
    with tempfile.TemporaryDirectory(prefix=f"client-baseline-{name}-") as temporary:
        scratch = Path(temporary)
        archive = scratch / "source.tar"
        checkout = scratch / "source"
        checkout.mkdir()
        subprocess.run(["git", "-C", str(source), "archive", "--format=tar", f"--output={archive}", revision], check=True)
        with tarfile.open(archive) as source_archive:
            source_archive.extractall(checkout, filter="data")
        for test in (ROOT / "internal/contracttest/baseline" / name).rglob("*.go.txt"):
            relative = test.relative_to(ROOT / "internal/contracttest/baseline" / name)
            destination = checkout / relative.with_suffix("")
            if destination.exists():
                raise ValueError(f"refusing to overwrite archived source: {destination}")
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(test, destination)
        environment = dict(os.environ, GOWORK="off", GOFLAGS="-mod=readonly")
        print(f"{name}: {revision}", flush=True)
        subprocess.run(["go", "test", "-race", "-count=1", *packages], cwd=checkout, env=environment, check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--console", type=Path, required=True, help="local console Git repository")
    parser.add_argument("--cli", type=Path, required=True, help="local CLI Git repository")
    arguments = parser.parse_args()
    run_baseline("console", arguments.console.resolve())
    run_baseline("cli", arguments.cli.resolve())


if __name__ == "__main__":
    main()
