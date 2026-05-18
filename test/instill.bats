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
  export SKILL_LIBRARY_PATH="$BATS_TEST_TMPDIR/library"
  mkdir -p "$SKILL_LIBRARY_PATH"
}

make_skill() {
  mkdir -p "$SKILL_LIBRARY_PATH/$1"
  printf '# %s\n' "$1" > "$SKILL_LIBRARY_PATH/$1/SKILL.md"
}

make_project() {
  PROJECT="$BATS_TEST_TMPDIR/project"
  mkdir -p "$PROJECT"
  cd "$PROJECT"
}

write_manifest() {
  mkdir -p .claude/skills
  printf '%s\n' "$1" > .claude/skill-manifest.json
}

run_with_tty() {
  if script -q /dev/null true >/dev/null 2>&1; then
    script -q /dev/null "$@"
  else
    local command=""
    printf -v command '%q ' "$@"
    script -q -e -c "$command" /dev/null
  fi
}

@test "check-skills creates missing links and removes orphan links" {
  make_skill docker
  make_skill golang-cli
  make_project
  write_manifest '{"skills":["docker"]}'
  ln -s "$SKILL_LIBRARY_PATH/golang-cli" .claude/skills/golang-cli

  run "$INSTILL_BIN" check-skills

  [ "$status" -eq 0 ]
  [[ "$output" == *"created: docker ->"* ]]
  [[ "$output" == *"ok: 1 skills linked"* ]]
  [ "$(readlink .claude/skills/docker)" = "$SKILL_LIBRARY_PATH/docker" ]
  [ ! -e .claude/skills/golang-cli ]
}

@test "check-skills removes manifest skills missing from the library" {
  make_skill docker
  make_project
  write_manifest '{"skills":["docker","missing"]}'

  run "$INSTILL_BIN" check-skills

  [ "$status" -eq 0 ]
  [[ "$output" == *"removed: missing (no longer in library)"* ]]
  [[ "$(cat .claude/skill-manifest.json)" != *"missing"* ]]
}

@test "check-skills grants and pick-skills revokes Claude skill permissions" {
  make_skill docker
  make_skill golang-cli
  make_project
  write_manifest '{"skills":["docker"]}'

  run "$INSTILL_BIN" check-skills

  [ "$status" -eq 0 ]
  [[ "$(cat .claude/settings.local.json)" == *'"Skill(docker)"'* ]]

  run "$INSTILL_BIN" pick-skills golang-cli --remove docker

  [ "$status" -eq 0 ]
  [[ "$(cat .claude/settings.local.json)" == *'"Skill(golang-cli)"'* ]]
  [[ "$(cat .claude/settings.local.json)" != *'"Skill(docker)"'* ]]
}

@test "init-project with --skills initializes, warns outside git, and links skills" {
  make_skill docker
  make_skill golang-cli
  make_project

  run "$INSTILL_BIN" init-project --skills docker,golang-cli

  [ "$status" -eq 0 ]
  [[ "$output" == *"initialized: .claude/skill-manifest.json"* ]]
  [[ "$output" == *"warning:     not a git repository"* ]]
  [[ "$output" == *"created: docker ->"* ]]
  [ "$(readlink .claude/skills/docker)" = "$SKILL_LIBRARY_PATH/docker" ]
  [[ "$(cat .gitignore)" == *".claude/skills/"* ]]
  [[ "$(cat .gitignore)" == *".claude/settings.local.json"* ]]
}

@test "init-project refuses an existing manifest without --force" {
  make_skill docker
  make_project
  write_manifest '{"skills":[]}'

  run "$INSTILL_BIN" init-project

  [ "$status" -eq 1 ]
  [[ "$output" == *"error: manifest already exists; use --force to reinitialize"* ]]
}

@test "init-project --force overwrites an existing manifest before launching TUI" {
  make_skill docker
  make_project
  write_manifest '{"skills":["docker"]}'

  run "$INSTILL_BIN" init-project --force

  [ "$status" -eq 2 ]
  [[ "$output" == *"error: pick-skills TUI requires a terminal"* ]]
  [[ "$(cat .claude/skill-manifest.json)" == *'"docker"'* ]]
  [ -d .claude/skills ]
}

@test "init-project without --skills in a non-TTY exits 2 without writes" {
  make_skill docker
  make_project

  run "$INSTILL_BIN" init-project

  [ "$status" -eq 2 ]
  [[ "$output" == *"error: pick-skills TUI requires a terminal"* ]]
  [ ! -e .claude ]
  [ ! -e .gitignore ]
}

@test "show-library works outside a project and supports filters" {
  make_skill bash-defensive-patterns
  make_skill docker
  make_skill golang-cli
  make_project

  run "$INSTILL_BIN" show-library --filter golang

  [ "$status" -eq 0 ]
  [[ "$output" == *"golang-cli"* ]]
  [[ "$output" != *"docker"* ]]
  [[ "$output" == *"1 skills"* ]]
}

@test "show-library annotates selections inside a project" {
  make_skill docker
  make_skill golang-cli
  make_project
  write_manifest '{"skills":["docker"]}'

  run "$INSTILL_BIN" show-library

  [ "$status" -eq 0 ]
  [[ "$output" == *"[✓] docker"* ]]
  [[ "$output" == *"[ ] golang-cli"* ]]
  [[ "$output" == *"2 skills  (1 selected)"* ]]
}

@test "pick-skills adds and removes skills atomically" {
  make_skill docker
  make_skill golang-cli
  make_project
  write_manifest '{"skills":["docker"]}'

  run "$INSTILL_BIN" pick-skills golang-cli --remove docker

  [ "$status" -eq 0 ]
  [[ "$output" == *"added:   golang-cli"* ]]
  [[ "$output" == *"removed: docker"* ]]
  [[ "$output" == *"manifest: 1 skills"* ]]
  [[ "$(cat .claude/skill-manifest.json)" == *"golang-cli"* ]]
  [[ "$(cat .claude/skill-manifest.json)" != *"docker"* ]]
}

@test "pick-skills unknown skill exits 1 and leaves manifest unchanged" {
  make_skill docker
  make_project
  write_manifest '{"skills":["docker"]}'

  run "$INSTILL_BIN" pick-skills missing

  [ "$status" -eq 1 ]
  [[ "$output" == *"error: unknown skill: missing"* ]]
  [[ "$(cat .claude/skill-manifest.json)" == *"docker"* ]]
  [[ "$(cat .claude/skill-manifest.json)" != *"missing"* ]]
}

@test "pick-skills without a manifest exits 1" {
  make_skill docker
  make_project

  run "$INSTILL_BIN" pick-skills docker

  [ "$status" -eq 1 ]
  [[ "$output" == *"error: no manifest found — run 'instill init-project' first"* ]]
}

@test "pick-skills no-args non-TTY exits 2 and leaves manifest unchanged" {
  make_skill docker
  make_skill golang-cli
  make_project
  write_manifest '{"skills":["docker"]}'

  run "$INSTILL_BIN" pick-skills

  [ "$status" -eq 2 ]
  [[ "$output" == *"error: pick-skills TUI requires a terminal"* ]]
  [[ "$(cat .claude/skill-manifest.json)" == *'"docker"'* ]]
  [[ "$(cat .claude/skill-manifest.json)" != *'"golang-cli"'* ]]
}

@test "add-hooks is a silent no-op without a TTY" {
  make_project

  run "$INSTILL_BIN" add-hooks

  [ "$status" -eq 0 ]
  [ "$output" = "" ]
  [ ! -e .claude/settings.json ]
}

@test "add-hooks adds the SessionStart hook and is idempotent with a TTY" {
  make_skill docker
  make_project
  write_manifest '{"skills":["docker"]}'

  run run_with_tty "$INSTILL_BIN" add-hooks

  [ "$status" -eq 0 ]
  [[ "$output" == *"added SessionStart hook: instill check-skills"* ]]
  [[ "$(cat .claude/settings.json)" == *"instill check-skills"* ]]

  run run_with_tty "$INSTILL_BIN" add-hooks

  [ "$status" -eq 0 ]
  [[ "$output" == *"already configured"* ]]
  [ "$(grep -c "instill check-skills" .claude/settings.json)" -eq 1 ]
}

@test "missing library configuration exits 2" {
  make_project
  unset SKILL_LIBRARY_PATH

  run "$INSTILL_BIN" show-library

  [ "$status" -eq 2 ]
  [[ "$output" == *"error: no library path configured; set SKILL_LIBRARY_PATH"* ]]
}

@test "filesystem collision while reconciling exits 3" {
  make_skill docker
  make_project
  write_manifest '{"skills":["docker"]}'
  printf 'not a symlink\n' > .claude/skills/docker

  run "$INSTILL_BIN" check-skills

  [ "$status" -eq 3 ]
  [[ "$output" == *"error: cannot remove non-symlink"* ]]
}

@test "errors are written to stderr for exit 1, 2, and 3" {
  make_project

  run bash -c '"$1" pick-skills docker 2>"$2"' _ "$INSTILL_BIN" "$BATS_TEST_TMPDIR/exit1.err"
  [ "$status" -eq 1 ]
  [ "$output" = "" ]
  [[ "$(cat "$BATS_TEST_TMPDIR/exit1.err")" == *"error: no manifest found"* ]]

  unset SKILL_LIBRARY_PATH
  run bash -c '"$1" show-library 2>"$2"' _ "$INSTILL_BIN" "$BATS_TEST_TMPDIR/exit2.err"
  [ "$status" -eq 2 ]
  [ "$output" = "" ]
  [[ "$(cat "$BATS_TEST_TMPDIR/exit2.err")" == *"error: no library path configured"* ]]

  export SKILL_LIBRARY_PATH="$BATS_TEST_TMPDIR/library"
  make_skill docker
  write_manifest '{"skills":["docker"]}'
  printf 'not a symlink\n' > .claude/skills/docker
  run bash -c '"$1" check-skills 2>"$2"' _ "$INSTILL_BIN" "$BATS_TEST_TMPDIR/exit3.err"
  [ "$status" -eq 3 ]
  [[ "$output" != *"error: cannot remove non-symlink"* ]]
  [[ "$(cat "$BATS_TEST_TMPDIR/exit3.err")" == *"error: cannot remove non-symlink"* ]]
}

make_group_skill() {
  mkdir -p "$SKILL_LIBRARY_PATH/$1/$2"
  printf '# %s/%s\n' "$1" "$2" > "$SKILL_LIBRARY_PATH/$1/$2/SKILL.md"
}

@test "pick-skills adds a qualified group skill and creates a flat colon symlink" {
  make_group_skill superpowers brainstorming
  make_project
  write_manifest '{"skills":[]}'

  run "$INSTILL_BIN" pick-skills superpowers/brainstorming

  [ "$status" -eq 0 ]
  [[ "$output" == *"added:   superpowers/brainstorming"* ]]
  [ "$(readlink ".claude/skills/superpowers:brainstorming")" = "$SKILL_LIBRARY_PATH/superpowers/brainstorming" ]
  [ ! -e .claude/skills/superpowers ]
  [[ "$(cat .claude/skill-manifest.json)" == *'"superpowers/brainstorming"'* ]]
}

@test "removing a qualified group skill removes the flat colon symlink" {
  make_skill docker
  make_group_skill superpowers brainstorming
  make_project
  write_manifest '{"skills":["docker","superpowers/brainstorming"]}'

  run "$INSTILL_BIN" check-skills
  [ "$status" -eq 0 ]

  run "$INSTILL_BIN" pick-skills --remove superpowers/brainstorming

  [ "$status" -eq 0 ]
  [ ! -e ".claude/skills/superpowers:brainstorming" ]
  [[ "$(cat .claude/skill-manifest.json)" != *"superpowers"* ]]
}

@test "removing one group skill leaves the other flat colon symlink intact" {
  make_skill docker
  make_group_skill superpowers brainstorming
  make_group_skill superpowers writing-plans
  make_project
  write_manifest '{"skills":["docker","superpowers/brainstorming","superpowers/writing-plans"]}'

  run "$INSTILL_BIN" check-skills
  [ "$status" -eq 0 ]

  run "$INSTILL_BIN" pick-skills --remove superpowers/brainstorming

  [ "$status" -eq 0 ]
  [ ! -e ".claude/skills/superpowers:brainstorming" ]
  [ "$(readlink ".claude/skills/superpowers:writing-plans")" = "$SKILL_LIBRARY_PATH/superpowers/writing-plans" ]
}
