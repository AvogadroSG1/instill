"""Shared helpers for executable E2E contract tests against the built instill binary."""

import atexit
import os
import pathlib
import pty
import re
import select
import shlex
import shutil
import stat
import subprocess
import tempfile
import time

_ANSI_RE = re.compile(r"\x1b\[[0-9;?]*[A-Za-z]")
_VERSION_RE = re.compile(r"(\d+)\.(\d+)\.(\d+)")


def find_apm_bin(min_version: tuple[int, int, int] = (0, 35, 0)) -> str:
    """Locate an `apm` binary meeting *min_version* from APM_BIN or PATH."""
    candidates: list[str] = []
    if env_bin := os.environ.get("APM_BIN"):
        candidates.append(env_bin)
    for directory in os.environ.get("PATH", "").split(os.pathsep):
        if not directory:
            continue
        candidate = os.path.join(directory, "apm")
        if os.path.isfile(candidate) and os.access(candidate, os.X_OK) and candidate not in candidates:
            candidates.append(candidate)

    seen_versions: list[str] = []
    for candidate in candidates:
        try:
            res = subprocess.run([candidate, "--version"], capture_output=True, text=True, timeout=5)
        except (OSError, subprocess.SubprocessError):
            continue
        match = _VERSION_RE.search(res.stdout + "\n" + res.stderr)
        if not match:
            continue
        ver = tuple(int(part) for part in match.groups())
        seen_versions.append(f"{candidate} ({'.'.join(map(str, ver))})")
        if ver >= min_version:
            return os.path.abspath(candidate)

    raise RuntimeError(
        f"no apm >= {'.'.join(map(str, min_version))} found in APM_BIN/PATH; checked: {', '.join(seen_versions) or 'none'}"
    )


def path_with_apm(apm_bin: str, base_path: str | None = None) -> str:
    apm_path = pathlib.Path(apm_bin)
    if apm_path.name != "apm":
        shim_dir = pathlib.Path(tempfile.mkdtemp(prefix="instill-apm-shim-"))
        atexit.register(shutil.rmtree, shim_dir, True)
        shim = shim_dir / "apm"
        shim.write_text(f"#!/bin/sh\nexec {shlex.quote(str(apm_path))} \"$@\"\n")
        shim.chmod(shim.stat().st_mode | stat.S_IEXEC)
        apm_dir = str(shim_dir)
    else:
        apm_dir = str(apm_path.parent)
    current = base_path if base_path is not None else os.environ.get("PATH", "")
    return apm_dir + os.pathsep + current


def run_overwrite_apply(binary: str, proj: pathlib.Path, env: dict[str, str], timeout: float = 30.0) -> tuple[int, str]:
    master, slave = pty.openpty()
    proc = subprocess.Popen(
        [binary, "pick", "--overwrite"],
        cwd=proj,
        env=env,
        stdin=slave,
        stdout=slave,
        stderr=slave,
        close_fds=True,
    )
    os.close(slave)
    out = bytearray()
    sent_apply = False
    deadline = time.time() + timeout
    while time.time() < deadline:
        r, _, _ = select.select([master], [], [], 0.1)
        if r:
            try:
                chunk = os.read(master, 4096)
                if not chunk:
                    break
                out.extend(chunk)
            except OSError:
                break
        text = _ANSI_RE.sub("", out.decode("utf-8", "replace"))
        if not sent_apply and "a applies the complete selection" in text:
            os.write(master, b"a")
            sent_apply = True
        if proc.poll() is not None:
            break
    while True:
        r, _, _ = select.select([master], [], [], 0.1)
        if not r:
            break
        try:
            chunk = os.read(master, 4096)
            if not chunk:
                break
            out.extend(chunk)
        except OSError:
            break
    os.close(master)
    if proc.poll() is None:
        proc.kill()
        raise RuntimeError("timed out waiting for instill pick --overwrite")
    return proc.returncode, _ANSI_RE.sub("", out.decode("utf-8", "replace"))
