"""Materialize GoReleaser's non-templatable target lists for the native host."""

import os
from pathlib import Path
import subprocess
import sys

host_os = subprocess.check_output(["go", "env", "GOHOSTOS"], text=True).strip()
host_arch = subprocess.check_output(["go", "env", "GOHOSTARCH"], text=True).strip()
for key, host in (("GOOS", host_os), ("GOARCH", host_arch)):
    if os.environ.get(key, host) != host:
        raise SystemExit(f"{key} must match the native host ({host})")
if (host_os, host_arch) not in {
    ("linux", "amd64"), ("linux", "arm64"),
    ("darwin", "amd64"), ("darwin", "arm64"), ("windows", "amd64"),
}:
    raise SystemExit(f"Unsupported native release target: {host_os}/{host_arch}")
config = Path(".goreleaser.yml").read_text()
config = config.replace("{{ .Env.GOOS }}", host_os).replace("{{ .Env.GOARCH }}", host_arch)
Path(sys.argv[1]).write_text(config)
