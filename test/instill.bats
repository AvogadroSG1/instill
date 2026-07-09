#!/usr/bin/env bats

setup_file() {
  export REPO_ROOT
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  export INSTILL_TEST_DIR
  INSTILL_TEST_DIR="$(mktemp -d)"
  export INSTILL_BIN="$INSTILL_TEST_DIR/instill"
  (cd "$REPO_ROOT" && go build -o "$INSTILL_BIN" .)
}

teardown_file() {
  rm -rf "$INSTILL_TEST_DIR"
}

setup() {
  export HOME="$BATS_TEST_TMPDIR/home"
  mkdir -p "$HOME"
  export INSTILL_LIBRARY_PATH="$BATS_TEST_TMPDIR/library"
  mkdir -p "$INSTILL_LIBRARY_PATH"
  install_fake_apm
}

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
    ;;
  compile)
    project="$(project_arg "$@")"
    mkdir -p "$project/.apm"
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

make_project() {
  PROJECT="$BATS_TEST_TMPDIR/project"
  mkdir -p "$PROJECT"
  cd "$PROJECT"
}

make_skill() {
  mkdir -p "$INSTILL_LIBRARY_PATH/skills/$1"
  printf '# %s\n' "$1" > "$INSTILL_LIBRARY_PATH/skills/$1/SKILL.md"
}

make_instruction() {
  mkdir -p "$INSTILL_LIBRARY_PATH/instructions/$1"
  printf '# %s instruction\n' "$1" > "$INSTILL_LIBRARY_PATH/instructions/$1/INSTRUCTION.md"
}

make_prompt() {
  mkdir -p "$INSTILL_LIBRARY_PATH/prompts/$1"
  printf '# %s prompt\n' "$1" > "$INSTILL_LIBRARY_PATH/prompts/$1/PROMPT.md"
}

make_mcp() {
  mkdir -p "$INSTILL_LIBRARY_PATH/mcp/$1"
  printf '{"transport":"stdio","command":"%s-mcp","args":["--dev"],"env":["DEV=true"]}\n' "$1" > "$INSTILL_LIBRARY_PATH/mcp/$1/config.json"
}

scan_library() {
  run "$INSTILL_BIN" library scan
  [ "$status" -eq 0 ]
}

write_legacy_manifest() {
  mkdir -p .claude/skills
  printf '%s\n' "$1" > .claude/skill-manifest.json
}

@test "library scan creates typed catalogs" {
  make_skill docker
  make_mcp local-db
  make_instruction python-rules
  make_prompt debug
  make_project

  scan_library

  [ -f "$INSTILL_LIBRARY_PATH/skills/catalog.csv" ]
  [ -f "$INSTILL_LIBRARY_PATH/mcp/catalog.csv" ]
  [ -f "$INSTILL_LIBRARY_PATH/instructions/catalog.csv" ]
  [ -f "$INSTILL_LIBRARY_PATH/prompts/catalog.csv" ]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/skills/catalog.csv")" == *"docker,,docker/SKILL.md"* ]]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/catalog.csv")" == *"local-db,stdio,local-db-mcp"* ]]
}

@test "init writes apm.yml and does not create legacy manifest or symlinks" {
  make_skill docker
  make_project
  scan_library

  run "$INSTILL_BIN" init --skills docker

  [ "$status" -eq 0 ]
  [ -f apm.yml ]
  [ ! -e .claude/skill-manifest.json ]
  [ ! -e .claude/skills/docker ]
  [[ "$(cat apm.yml)" == *"name: project"* ]]
  [[ "$(cat apm.yml)" == *"version: 0.1.0"* ]]
  [[ "$(cat apm.yml)" == *"dependencies:"* ]]
  [[ "$(cat apm.yml)" == *"$INSTILL_LIBRARY_PATH/skills/docker"* ]]
}

@test "pick copies typed local content and sync reports APM-backed counts" {
  make_skill docker
  make_instruction python-rules
  make_prompt debug
  make_project
  scan_library

  run "$INSTILL_BIN" init --skills docker
  [ "$status" -eq 0 ]

  run "$INSTILL_BIN" pick --type instruction python-rules
  [ "$status" -eq 0 ]
  [ -f .apm/instructions/python-rules.instructions.md ]
  [[ "$(cat .apm/instructions/python-rules.instructions.md)" == *"python-rules instruction"* ]]

  run "$INSTILL_BIN" pick --type prompt debug
  [ "$status" -eq 0 ]
  [ -f .apm/prompts/debug.prompt.md ]
  [[ "$(cat .apm/prompts/debug.prompt.md)" == *"debug prompt"* ]]

  run "$INSTILL_BIN" sync
  [ "$status" -eq 0 ]
  [[ "$output" == *"ok: synced 1 skills, 0 mcp servers, 1 instructions, 1 prompts"* ]]
}

@test "pick adds MCP catalog entries to the APM manifest" {
  make_mcp local-db
  make_project
  scan_library

  run "$INSTILL_BIN" init --force --skills ""
  [ "$status" -eq 2 ]
  [[ "$output" == *"error: pick TUI requires a terminal"* ]]

  printf 'dependencies: {}\n' > apm.yml
  run "$INSTILL_BIN" pick --type mcp local-db

  [ "$status" -eq 0 ]
  [[ "$(cat apm.yml)" == *"name: local-db"* ]]
  [[ "$(cat apm.yml)" == *"command: local-db-mcp"* ]]
}

@test "add-hooks registers instill sync and replaces legacy managed hook" {
  make_project
  mkdir -p .claude
  printf 'dependencies: {}\n' > apm.yml
  printf '{"hooks":{"SessionStart":[{"matcher":"","hooks":[{"type":"command","command":"instill check-skills"}]}]}}\n' > .claude/settings.json

  run bash -c 'if script -q /dev/null true >/dev/null 2>&1; then script -q /dev/null "$1" add-hooks; else script -q -e -c "$1 add-hooks" /dev/null; fi' _ "$INSTILL_BIN"

  [ "$status" -eq 0 ]
  [[ "$output" == *"added SessionStart hook: instill sync"* ]]
  [[ "$(cat .claude/settings.json)" == *"instill sync"* ]]
  [[ "$(cat .claude/settings.json)" != *"instill check-skills"* ]]
}

@test "legacy check-skills exits with migration guidance and does not mutate state" {
  make_skill docker
  make_project
  write_legacy_manifest '{"skills":["docker"]}'
  ln -s /legacy/orphan .claude/skills/orphan
  before="$(cat .claude/skill-manifest.json)"

  run "$INSTILL_BIN" check-skills

  [ "$status" -eq 1 ]
  [[ "$output" == *"check-skills has been replaced by 'instill sync'"* ]]
  [ "$(cat .claude/skill-manifest.json)" = "$before" ]
  [ ! -e .claude/skills/docker ]
  [ "$(readlink .claude/skills/orphan)" = "/legacy/orphan" ]
}

@test "full init pick sync flow creates all expected artifacts" {
  make_skill docker
  make_skill golang-testing
  make_mcp local-db
  make_instruction python-rules
  make_prompt debug
  make_project
  scan_library

  # Init with one skill
  run "$INSTILL_BIN" init --skills docker
  [ "$status" -eq 0 ]
  [ -f apm.yml ]
  [[ "$(cat apm.yml)" == *"name: project"* ]]
  [[ "$(cat apm.yml)" == *"docker"* ]]

  # Pick additional skill
  run "$INSTILL_BIN" pick --type skill golang-testing
  [ "$status" -eq 0 ]
  [[ "$(cat apm.yml)" == *"golang-testing"* ]]

  # Pick MCP server
  run "$INSTILL_BIN" pick --type mcp local-db
  [ "$status" -eq 0 ]
  [[ "$(cat apm.yml)" == *"local-db"* ]]

  # Pick instruction
  run "$INSTILL_BIN" pick --type instruction python-rules
  [ "$status" -eq 0 ]
  [ -f .apm/instructions/python-rules.instructions.md ]

  # Pick prompt
  run "$INSTILL_BIN" pick --type prompt debug
  [ "$status" -eq 0 ]
  [ -f .apm/prompts/debug.prompt.md ]

  # Sync exercises apm install + compile
  run "$INSTILL_BIN" sync
  [ "$status" -eq 0 ]
  [[ "$output" == *"ok: synced"* ]]

  # Verify APM artifacts created by fake apm
  [ -f apm.lock.yaml ]
  [ -d .apm ]

  # Name and version fields preserved through picks
  [[ "$(cat apm.yml)" == *"name: project"* ]]
  [[ "$(cat apm.yml)" == *"version: 0.1.0"* ]]

  # Remove a skill and verify prune path
  run "$INSTILL_BIN" pick --type skill --remove docker
  [ "$status" -eq 0 ]
  [[ "$(cat apm.yml)" != *"docker"* ]]
  [[ "$(cat apm.yml)" == *"golang-testing"* ]]
}
