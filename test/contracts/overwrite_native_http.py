#!/usr/bin/env python3
"""Executable E2E contract for instill-bh9: native HTTP MCP overwrite verification
against real APM (Gemini httpUrl, Antigravity serverUrl)."""

import os
import pathlib
import shutil
import subprocess
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from _common import find_apm_bin, path_with_apm, run_overwrite_apply


def main() -> None:
    binary = sys.argv[1]
    apm_bin = find_apm_bin()
    base = pathlib.Path(tempfile.mkdtemp(prefix="instill-bh9-contract-"))
    failures = 0
    try:
        lib = base / "library"
        (lib / "mcp" / "remote-probe").mkdir(parents=True)
        (lib / "mcp" / "remote-probe" / "config.json").write_text(
            '{"transport":"http","url":"https://example.invalid/mcp",'
            '"env":["TOKEN=${TOKEN}"],"headers":["Authorization=Bearer ${TOKEN}"],'
            '"default_enabled":true}'
        )
        subprocess.run(
            [binary, "library", "scan"],
            env={**os.environ, "INSTILL_LIBRARY_PATH": str(lib)},
            check=True,
            capture_output=True,
        )

        for target, cfg_rel, expected_key in [
            ("gemini", ".gemini/settings.json", "httpUrl"),
            ("antigravity", ".agents/mcp_config.json", "serverUrl"),
        ]:
            proj = base / f"proj-{target}"
            proj.mkdir()
            (proj / "apm.yml").write_text(
                f"name: proj-{target}\nversion: 1.0.0\ntargets:\n  - {target}\n"
                "dependencies:\n  apm: []\n  mcp:\n"
                "    - name: remote-probe\n      registry: false\n      transport: http\n"
                "      url: https://example.invalid/mcp\n"
                "      env:\n        TOKEN: \"${TOKEN}\"\n"
                "      headers:\n        Authorization: \"Bearer ${TOKEN}\"\n"
            )
            (proj / pathlib.Path(cfg_rel).parent).mkdir(parents=True, exist_ok=True)
            env = {
                "PATH": path_with_apm(apm_bin),
                "HOME": str(proj),
                "INSTILL_LIBRARY_PATH": str(lib),
                "TOKEN": "instill-smoke-sentinel",
                "TERM": "xterm-256color",
            }
            rc, out = run_overwrite_apply(binary, proj, env)
            cfg = proj / cfg_rel
            if rc != 0 or not cfg.exists() or expected_key not in cfg.read_text():
                print(f"FAIL [{target}]: rc={rc} cfg_exists={cfg.exists()}")
                print(out[-400:].strip())
                failures += 1
            else:
                print(f"PASS [{target}]: rc=0, verified {cfg_rel} contains {expected_key}")
    finally:
        shutil.rmtree(base, ignore_errors=True)
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
