#!/usr/bin/env python3
"""Executable E2E contract for instill-ekf: verify overwrite headers resolved by
APM at install time (single-pass env expansion) without accepting wrong headers,
unset-env literals, or wrong URLs."""

import json
import os
import pathlib
import shutil
import stat
import subprocess
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from _common import find_apm_bin, path_with_apm, run_overwrite_apply


def make_apm_wrapper(wrapper_dir: pathlib.Path, real_apm: str, cfg_rel: str, mutate_mode: str) -> None:
    wrapper = wrapper_dir / "apm"
    wrapper.write_text(f"""#!/usr/bin/env python3
import json, os, pathlib, subprocess, sys
rc = subprocess.call([{real_apm!r}] + sys.argv[1:])
if rc == 0 and len(sys.argv) > 1 and sys.argv[1] == "install":
    cfg = pathlib.Path(os.getcwd()) / {cfg_rel!r}
    if cfg.exists():
        data = json.loads(cfg.read_text())
        srv = data.get("mcpServers", {{}}).get("remote-probe", {{}})
        mode = {mutate_mode!r}
        if mode == "resolve_token":
            srv["headers"]["Authorization"] = "Bearer " + os.environ.get("TOKEN", "instill-smoke-sentinel")
        elif mode == "wrong_header":
            srv["headers"]["Authorization"] = "Bearer wrong-token"
        elif mode == "wrong_url":
            srv["headers"]["Authorization"] = "Bearer " + os.environ.get("TOKEN", "instill-smoke-sentinel")
            for k in ("url", "httpUrl", "serverUrl"):
                if k in srv:
                    srv[k] = "https://evil.invalid/mcp"
        cfg.write_text(json.dumps(data, indent=2))
sys.exit(rc)
""")
    wrapper.chmod(wrapper.stat().st_mode | stat.S_IEXEC)


def main() -> None:
    binary = sys.argv[1]
    real_apm = find_apm_bin()
    base = pathlib.Path(tempfile.mkdtemp(prefix="instill-ekf-contract-"))
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

        cases = [
            ("real-apm claude preserved", "claude", ".mcp.json", None, "instill-smoke-sentinel", True),
            ("real-apm gemini preserved", "gemini", ".gemini/settings.json", None, "instill-smoke-sentinel", True),
            ("real-apm antigravity preserved", "antigravity", ".agents/mcp_config.json", None, "instill-smoke-sentinel", True),
            ("gemini resolved token passes", "gemini", ".gemini/settings.json", "resolve_token", "instill-smoke-sentinel", True),
            ("antigravity resolved token passes", "antigravity", ".agents/mcp_config.json", "resolve_token", "instill-smoke-sentinel", True),
            ("gemini wrong header rejected", "gemini", ".gemini/settings.json", "wrong_header", "instill-smoke-sentinel", False),
            ("gemini unset token literal rejected", "gemini", ".gemini/settings.json", "resolve_token", None, False),
            ("gemini wrong url rejected", "gemini", ".gemini/settings.json", "wrong_url", "instill-smoke-sentinel", False),
        ]

        for idx, (label, target, cfg_rel, mode, token_env, want_ok) in enumerate(cases):
            proj = base / f"case-{idx}"
            proj.mkdir()
            (proj / "apm.yml").write_text(
                f"name: case-{idx}\nversion: 1.0.0\ntargets:\n  - {target}\n"
                "dependencies:\n  apm: []\n  mcp:\n"
                "    - name: remote-probe\n      registry: false\n      transport: http\n"
                "      url: https://example.invalid/mcp\n"
                "      env:\n        TOKEN: \"${TOKEN}\"\n"
                "      headers:\n        Authorization: \"Bearer ${TOKEN}\"\n"
            )
            parent = proj / pathlib.Path(cfg_rel).parent
            if parent != proj:
                parent.mkdir(parents=True, exist_ok=True)
            path_env = path_with_apm(real_apm)
            if mode is not None:
                wdir = base / f"wrap-{idx}"
                wdir.mkdir()
                make_apm_wrapper(wdir, real_apm, cfg_rel, mode)
                path_env = str(wdir) + os.pathsep + path_env
            env = {
                "PATH": path_env,
                "HOME": str(proj),
                "INSTILL_LIBRARY_PATH": str(lib),
                "TERM": "xterm-256color",
            }
            if token_env is not None:
                env["TOKEN"] = token_env
            rc, out = run_overwrite_apply(binary, proj, env)
            ok = (rc == 0) == want_ok
            if ok and not want_ok:
                expected_line = f'error: project MCP definition for {target} does not match selected server "remote-probe"'
                if expected_line not in {line.strip() for line in out.splitlines()}:
                    ok = False
                    print(f"  missing expected error line: {expected_line}")
                cfg_path = proj / cfg_rel
                if not cfg_path.exists():
                    ok = False
                    print(f"  expected mutated config {cfg_rel} to exist")
                else:
                    srv = json.loads(cfg_path.read_text()).get("mcpServers", {}).get("remote-probe", {})
                    auth = srv.get("headers", {}).get("Authorization")
                    url = srv.get("httpUrl") or srv.get("serverUrl") or srv.get("url")
                    if mode == "wrong_header":
                        if auth != "Bearer wrong-token" or url != "https://example.invalid/mcp":
                            ok = False
                            print(f"  unexpected wrong_header mutation: auth={auth!r} url={url!r}")
                    elif mode == "wrong_url":
                        if auth != "Bearer instill-smoke-sentinel" or url != "https://evil.invalid/mcp":
                            ok = False
                            print(f"  unexpected wrong_url mutation: auth={auth!r} url={url!r}")
                    elif mode == "resolve_token":
                        if auth != "Bearer instill-smoke-sentinel" or url != "https://example.invalid/mcp":
                            ok = False
                            print(f"  unexpected resolve_token mutation: auth={auth!r} url={url!r}")
            status = "PASS" if ok else "FAIL"
            print(f"{status} [{label}]: rc={rc} want_ok={want_ok}")
            if not ok:
                print("  tail:", out[-300:].strip())
                failures += 1
    finally:
        shutil.rmtree(base, ignore_errors=True)
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
