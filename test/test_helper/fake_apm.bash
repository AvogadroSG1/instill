#!/usr/bin/env bash

install_fake_apm() {
  mkdir -p "$BATS_TEST_TMPDIR/bin"
  export PATH="$BATS_TEST_TMPDIR/bin:$PATH"
  cat > "$BATS_TEST_TMPDIR/bin/apm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

project_arg() {
  local previous=""
  for value in "$@"; do
    if [ "$previous" = "--root" ]; then
      printf '%s\n' "$value"
      return 0
    fi
    previous="$value"
  done
  return 1
}

case "${1:-}" in
  --version)
    printf 'apm 0.1.0\n'
    ;;
  install)
    project="$(project_arg "$@")"
    mkdir -p "$project/.apm"
    touch "$project/apm.lock.yaml"
    for harness in .claude .agents; do
      mkdir -p "$project/$harness"
      printf 'installed\n' > "$project/$harness/.apm-installed"
    done
    ;;
  compile)
    project="$(project_arg "$@")"
    mkdir -p "$project/.apm"
    for harness in .claude .agents; do
      mkdir -p "$project/$harness"
      printf 'compiled\n' > "$project/$harness/.apm-compiled"
    done
    ;;
  prune)
    project="$(project_arg "$@")"
    mkdir -p "$project/.apm"
    ;;
  *)
    printf 'unexpected apm command: %s\n' "$*" >&2
    exit 1
    ;;
esac
EOF
  chmod +x "$BATS_TEST_TMPDIR/bin/apm"
}

install_fake_apm_claude_only() {
  mkdir -p "$BATS_TEST_TMPDIR/bin"
  export PATH="$BATS_TEST_TMPDIR/bin:$PATH"
  cat > "$BATS_TEST_TMPDIR/bin/apm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

project_arg() {
  local previous=""
  for value in "$@"; do
    if [ "$previous" = "--root" ]; then
      printf '%s\n' "$value"
      return 0
    fi
    previous="$value"
  done
  return 1
}

case "${1:-}" in
  --version)
    printf 'apm 0.1.0\n'
    ;;
  install)
    project="$(project_arg "$@")"
    mkdir -p "$project/.apm"
    touch "$project/apm.lock.yaml"
    mkdir -p "$project/.claude"
    printf 'installed\n' > "$project/.claude/.apm-installed"
    ;;
  compile)
    project="$(project_arg "$@")"
    mkdir -p "$project/.apm"
    mkdir -p "$project/.claude"
    printf 'compiled\n' > "$project/.claude/.apm-compiled"
    ;;
  prune)
    project="$(project_arg "$@")"
    mkdir -p "$project/.apm"
    ;;
  *)
    printf 'unexpected apm command: %s\n' "$*" >&2
    exit 1
    ;;
esac
EOF
  chmod +x "$BATS_TEST_TMPDIR/bin/apm"
}
