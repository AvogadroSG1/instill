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

has_flag() {
  local wanted="$1"
  shift
  for value in "$@"; do
    if [ "$value" = "$wanted" ]; then
      return 0
    fi
  done
  return 1
}

deploy_skill_deps() {
  local project="$1"
  shift
  local dep name
  while IFS= read -r dep; do
    [ -n "$dep" ] || continue
    [ -f "$dep/SKILL.md" ] || continue
    name="$(basename "$dep")"
    if has_flag "--legacy-skill-paths" "$@"; then
      mkdir -p "$project/.agents/skills/$name"
      cp "$dep/SKILL.md" "$project/.agents/skills/$name/SKILL.md"
    else
      rm -rf "$project/.agents/skills/$name"
      mkdir -p "$project/.agents/skills"
      cp -R "$dep" "$project/.agents/skills/$name"
    fi
  done < <(grep -E '^[[:space:]]*-[[:space:]]+/' "$project/apm.yml" 2>/dev/null | sed -E 's/^[[:space:]]*-[[:space:]]+//' || true)
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
    deploy_skill_deps "$project" "$@"
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
    project="$(pwd)"
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

install_fake_apm_strict() {
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

has_flag() {
  local wanted="$1"
  shift
  for value in "$@"; do
    if [ "$value" = "$wanted" ]; then
      return 0
    fi
  done
  return 1
}

deploy_skill_deps() {
  local project="$1"
  shift
  local dep name
  while IFS= read -r dep; do
    [ -n "$dep" ] || continue
    [ -f "$dep/SKILL.md" ] || continue
    name="$(basename "$dep")"
    if has_flag "--legacy-skill-paths" "$@"; then
      mkdir -p "$project/.agents/skills/$name"
      cp "$dep/SKILL.md" "$project/.agents/skills/$name/SKILL.md"
    else
      rm -rf "$project/.agents/skills/$name"
      mkdir -p "$project/.agents/skills"
      cp -R "$dep" "$project/.agents/skills/$name"
    fi
  done < <(grep -E '^[[:space:]]*-[[:space:]]+/' "$project/apm.yml" 2>/dev/null | sed -E 's/^[[:space:]]*-[[:space:]]+//' || true)
}

case "${1:-}" in
  --version)
    printf 'apm 0.1.0\n'
    ;;
  install)
    project="$(project_arg "$@")"
    harness_count=0
    for dir in .claude .codex .gemini .opencode .windsurf .kiro .cursor; do
      [ -d "$project/$dir" ] && harness_count=$((harness_count + 1))
    done
    if [ "$harness_count" -gt 1 ]; then
      if ! grep -q '^targets:' "$project/apm.yml" 2>/dev/null; then
        printf 'APM found signals for multiple harnesses but cannot decide which to deploy to.\nPin your target explicitly.\n' >&2
        exit 1
      fi
    fi
    mkdir -p "$project/.apm"
    touch "$project/apm.lock.yaml"
    for harness in .claude .agents; do
      mkdir -p "$project/$harness"
      printf 'installed\n' > "$project/$harness/.apm-installed"
    done
    deploy_skill_deps "$project" "$@"
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
    project="$(pwd)"
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
    while IFS= read -r dep; do
      [ -n "$dep" ] || continue
      [ -f "$dep/SKILL.md" ] || continue
      name="$(basename "$dep")"
      rm -rf "$project/.claude/skills/$name"
      mkdir -p "$project/.claude/skills"
      cp -R "$dep" "$project/.claude/skills/$name"
    done < <(grep -E '^[[:space:]]*-[[:space:]]+/' "$project/apm.yml" 2>/dev/null | sed -E 's/^[[:space:]]*-[[:space:]]+//' || true)
    ;;
  compile)
    project="$(project_arg "$@")"
    mkdir -p "$project/.apm"
    mkdir -p "$project/.claude"
    printf 'compiled\n' > "$project/.claude/.apm-compiled"
    ;;
  prune)
    project="$(pwd)"
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
