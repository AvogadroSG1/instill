#!/usr/bin/env python3
"""Executable E2E contract for instill-f96: audit and enforce SSE native transport
compatibility across APM 0.39.0 targets.

- Unsupported SSE targets (codex, antigravity) MUST fail BEFORE resetting project files.
- Supported SSE targets (claude, gemini, cursor, kiro, opencode) MUST succeed and verify
  against real APM 0.39.0 output.
- Tampered transport on targets that preserve the SSE discriminator (claude type=http,
  gemini httpUrl) MUST be rejected by runtime verification.
"""

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


def make_tamper_wrapper(wrapper_dir: pathlib.Path, real_apm: str, cfg_rel: str, mode: str) -> None:
    wrapper = wrapper_dir / "apm"
    wrapper.write_text(f"""#!/usr/bin/env python3
import json, os, pathlib, subprocess, sys
rc = subprocess.call([{real_apm!r}] + sys.argv[1:])
if rc == 0 and len(sys.argv) > 1 and sys.argv[1] == "install":
    cfg = pathlib.Path(os.getcwd()) / {cfg_rel!r}
    if cfg.exists():
        data = json.loads(cfg.read_text())
        srv = data.get("mcpServers", {{}}).get("sse-probe", {{}})
        if {mode!r} == "claude_to_http":
            srv["type"] = "http"
        elif {mode!r} == "gemini_to_httpurl":
            url = srv.pop("url", "https://example.invalid/sse")
            srv["httpUrl"] = url
        cfg.write_text(json.dumps(data, indent=2))
sys.exit(rc)
""")
    wrapper.chmod(wrapper.stat().st_mode | stat.S_IEXEC)

def main() -> None:
    binary = sys.argv[1]
    real_apm = find_apm_bin((0, 39, 0))
    base = pathlib.Path(tempfile.mkdtemp(prefix="instill-f96-contract-"))
    failures = 0
    try:
        lib = base / "library"
        (lib / "mcp" / "sse-probe").mkdir(parents=True)
        (lib / "mcp" / "sse-probe" / "config.json").write_text(
            '{"transport":"sse","url":"https://example.invalid/sse",'
            '"env":["TOKEN=${TOKEN}"],"headers":["X-Probe=literal"],'
            '"default_enabled":true}'
        )
        subprocess.run(
            [binary, "library", "scan"],
            env={**os.environ, "INSTILL_LIBRARY_PATH": str(lib)},
            check=True,
            capture_output=True,
        )

        # 1. Unsupported SSE targets must fail BEFORE resetting existing project files
        unsupported = [
            ("codex", ".codex/config.toml", '[mcp_servers.sentinel]\ncommand = "/bin/true"\n'),
            ("antigravity", ".agents/mcp_config.json", '{"mcpServers":{"sentinel":{"command":"/bin/true"}}}\n'),
        ]
        for target, cfg_rel, sentinel_content in unsupported:
            proj = base / f"unsupported-{target}"
            proj.mkdir()
            orig_manifest = (
                f"name: proj-{target}\nversion: 1.0.0\ntargets:\n  - {target}\n"
                "dependencies:\n  apm: []\n  mcp:\n"
                "    - name: sse-probe\n      registry: false\n      transport: sse\n"
                "      url: https://example.invalid/sse\n"
                "      env:\n        TOKEN: \"${TOKEN}\"\n"
                "      headers:\n        X-Probe: literal\n"
            )
            (proj / "apm.yml").write_text(orig_manifest)
            cfg = proj / cfg_rel
            cfg.parent.mkdir(parents=True, exist_ok=True)
            cfg.write_text(sentinel_content)
            env = {
                "PATH": path_with_apm(real_apm),
                "HOME": str(proj),
                "INSTILL_LIBRARY_PATH": str(lib),
                "TERM": "xterm-256color",
            }
            rc, out = run_overwrite_apply(binary, proj, env)
            preserved_cfg = cfg.exists() and cfg.read_text() == sentinel_content
            preserved_manifest = (proj / "apm.yml").read_text() == orig_manifest
            if rc != 0 and preserved_cfg and preserved_manifest and "sse" in out.lower():
                print(f"PASS [unsupported {target}]: rc={rc}, failed pre-reset with sentinel intact")
            else:
                print(
                    f"FAIL [unsupported {target}]: rc={rc} preserved_cfg={preserved_cfg} "
                    f"preserved_manifest={preserved_manifest}"
                )
                print("  tail:", out[-300:].strip())
                failures += 1

        # 2. Supported SSE targets must succeed against real APM 0.39.0
        supported = [
            ("claude", ".mcp.json"),
            ("gemini", ".gemini/settings.json"),
            ("cursor", ".cursor/mcp.json"),
            ("kiro", ".kiro/settings/mcp.json"),
            ("opencode", "opencode.json"),
        ]
        for target, cfg_rel in supported:
            proj = base / f"supported-{target}"
            proj.mkdir()
            (proj / "apm.yml").write_text(
                f"name: proj-{target}\nversion: 1.0.0\ntargets:\n  - {target}\n"
                "dependencies:\n  apm: []\n  mcp:\n"
                "    - name: sse-probe\n      registry: false\n      transport: sse\n"
                "      url: https://example.invalid/sse\n"
                "      env:\n        TOKEN: \"${TOKEN}\"\n"
                "      headers:\n        X-Probe: literal\n"
            )
            parent = proj / pathlib.Path(cfg_rel).parent
            if parent != proj:
                parent.mkdir(parents=True, exist_ok=True)
            env = {
                "PATH": path_with_apm(real_apm),
                "HOME": str(proj),
                "INSTILL_LIBRARY_PATH": str(lib),
                "TERM": "xterm-256color",
            }
            rc, out = run_overwrite_apply(binary, proj, env)
            cfg = proj / cfg_rel
            if rc == 0 and cfg.exists() and "sse-probe" in cfg.read_text():
                print(f"PASS [supported {target}]: rc=0, verified {cfg_rel}")
            else:
                print(f"FAIL [supported {target}]: rc={rc} cfg_exists={cfg.exists()}")
                print("  tail:", out[-300:].strip())
                failures += 1

        # 3. Negative checks: tampered transport on discriminator-preserving targets must be rejected
        for label, target, cfg_rel, mode in [
            ("claude tampered to http rejected", "claude", ".mcp.json", "claude_to_http"),
            ("gemini tampered to httpUrl rejected", "gemini", ".gemini/settings.json", "gemini_to_httpurl"),
        ]:
            proj = base / f"neg-{target}"
            proj.mkdir()
            (proj / "apm.yml").write_text(
                f"name: neg-{target}\nversion: 1.0.0\ntargets:\n  - {target}\n"
                "dependencies:\n  apm: []\n  mcp:\n"
                "    - name: sse-probe\n      registry: false\n      transport: sse\n"
                "      url: https://example.invalid/sse\n"
                "      env:\n        TOKEN: \"${TOKEN}\"\n"
                "      headers:\n        X-Probe: literal\n"
            )
            parent = proj / pathlib.Path(cfg_rel).parent
            if parent != proj:
                parent.mkdir(parents=True, exist_ok=True)
            wdir = base / f"wrap-{target}"
            wdir.mkdir()
            make_tamper_wrapper(wdir, real_apm, cfg_rel, mode)
            env = {
                "PATH": str(wdir) + os.pathsep + path_with_apm(real_apm),
                "HOME": str(proj),
                "INSTILL_LIBRARY_PATH": str(lib),
                "TERM": "xterm-256color",
            }
            rc, out = run_overwrite_apply(binary, proj, env)
            expected_line = f'error: project MCP definition for {target} does not match selected server "sse-probe"'
            cfg = proj / cfg_rel
            mutated_ok = False
            if cfg.exists():
                srv = json.loads(cfg.read_text()).get("mcpServers", {}).get("sse-probe", {})
                if mode == "claude_to_http":
                    mutated_ok = srv.get("type") == "http"
                elif mode == "gemini_to_httpurl":
                    mutated_ok = "httpUrl" in srv and "url" not in srv
            if rc != 0 and mutated_ok and expected_line in {line.strip() for line in out.splitlines()}:
                print(f"PASS [{label}]: rc={rc}, rejected tampered transport")
            else:
                print(f"FAIL [{label}]: rc={rc} mutated_ok={mutated_ok}")
                print("  tail:", out[-300:].strip())
                failures += 1
    finally:
        shutil.rmtree(base, ignore_errors=True)
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
