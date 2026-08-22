#!/usr/bin/env bats

load test_helper/common
load test_helper/fake_apm
load test_helper/harness

setup_file() {
  export REPO_ROOT
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  export INSTILL_TEST_DIR
  INSTILL_TEST_DIR="$(mktemp -d)"
  export INSTILL_BIN="$INSTILL_TEST_DIR/instill"
  (cd "$REPO_ROOT" && go build -o "$INSTILL_BIN" .)
  export MANIFEST_SEMANTICS_BIN="$INSTILL_TEST_DIR/manifest-semantics"
  (cd "$REPO_ROOT" && go build -o "$MANIFEST_SEMANTICS_BIN" ./test/test_helper/manifest_semantics.go)
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
  [[ "$output" == *"ok: synced 1 skills, 0 plugins, 0 mcp servers, 1 instructions, 1 prompts"* ]]
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

@test "pick rejects removal of user-owned MCP dependency missing from catalog" {
  make_project
  scan_library
  cat > apm.yml <<'YAML'
name: project
version: 1.0.0
dependencies:
  mcp:
    - {name: user-server, registry: true, x-owner: user}
YAML
  before="$(cat apm.yml)"

  run "$INSTILL_BIN" pick --type mcp --remove user-server

  [ "$status" -ne 0 ]
  [[ "$output" == *"unknown mcp: user-server"* ]]
  [ "$(cat apm.yml)" = "$before" ]
}

@test "pick preserves unknown manifest nodes" {
  make_skill docker
  make_mcp local-db
  mkdir -p "$INSTILL_LIBRARY_PATH/plugins/example/.claude-plugin"
  printf '{"name":"example"}\n' > "$INSTILL_LIBRARY_PATH/plugins/example/.claude-plugin/plugin.json"
  make_project
  scan_library
  cat > apm.yml <<'YAML'
# preserved head
name: project
version: 1.0.0
x-flow: {owner: user, values: [one, two]}
dependencies:
  lsp: [{name: gopls, x-user: true}]
  apm:
    - owner/remote#main
    - {marketplace: private, name: opaque}
  mcp:
    - io.example/opaque
    - {x-custom: true}
YAML

  run "$INSTILL_BIN" pick --type skill docker
  [ "$status" -eq 0 ]
  run "$MANIFEST_SEMANTICS_BIN" apm.yml
  [ "$status" -eq 0 ]
  run "$INSTILL_BIN" pick --type plugin example
  [ "$status" -eq 0 ]
  run "$MANIFEST_SEMANTICS_BIN" apm.yml
  [ "$status" -eq 0 ]
  run "$INSTILL_BIN" pick --type mcp local-db
  [ "$status" -eq 0 ]
  run "$MANIFEST_SEMANTICS_BIN" apm.yml
  [ "$status" -eq 0 ]

  manifest="$(cat apm.yml)"
  [[ "$manifest" == *"# preserved head"* ]]
  [[ "$manifest" == *"x-flow:"* ]]
  [[ "$manifest" == *"lsp:"* ]]
  [[ "$manifest" == *"owner/remote#main"* ]]
  [[ "$manifest" == *"marketplace: private"* ]]
  [[ "$manifest" == *"io.example/opaque"* ]]
  [[ "$manifest" == *"x-custom: true"* ]]
}

@test "sync preserves unknown manifest nodes in one reconciliation" {
  make_mcp local-db
  make_project
  mkdir -p .codex
  scan_library
  cat > apm.yml <<'YAML'
x-user: {flow: [one, two]}
dependencies:
  lsp: [{name: gopls}]
  apm: [owner/remote#main]
  mcp:
    - {name: local-db, registry: true, command: stale, x-owner: user}
    - io.example/opaque
YAML

  run "$INSTILL_BIN" sync
  [ "$status" -eq 0 ]

  manifest="$(cat apm.yml)"
  [[ "$manifest" == *"targets:"* ]]
  [[ "$manifest" == *"codex"* ]]
  [[ "$manifest" == *"registry: false"* ]]
  [[ "$manifest" == *"command: local-db-mcp"* ]]
  [[ "$manifest" == *"x-user:"* ]]
  [[ "$manifest" == *"lsp:"* ]]
  [[ "$manifest" == *"owner/remote#main"* ]]
  [[ "$manifest" == *"x-owner: user"* ]]
  [[ "$manifest" == *"io.example/opaque"* ]]
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

# ──────────────────────────────────────────────────────────────────────────────
# E2E: init with all four types creates complete project state
# ──────────────────────────────────────────────────────────────────────────────

@test "init with all four types creates complete project state" {
  make_skill docker
  make_mcp local-db
  make_instruction python-rules
  make_prompt debug
  make_project
  scan_library

  # Init with one skill via non-interactive flag
  run "$INSTILL_BIN" init --skills docker
  [ "$status" -eq 0 ]
  [ -f apm.yml ]
  [[ "$(cat apm.yml)" == *"name: project"* ]]
  [[ "$(cat apm.yml)" == *"version: 0.1.0"* ]]
  [[ "$(cat apm.yml)" == *"$INSTILL_LIBRARY_PATH/skills/docker"* ]]

  # Pick one MCP server
  run "$INSTILL_BIN" pick --type mcp local-db
  [ "$status" -eq 0 ]
  [[ "$(cat apm.yml)" == *"name: local-db"* ]]
  [[ "$(cat apm.yml)" == *"command: local-db-mcp"* ]]

  # Pick one instruction
  run "$INSTILL_BIN" pick --type instruction python-rules
  [ "$status" -eq 0 ]
  [ -f .apm/instructions/python-rules.instructions.md ]
  [[ "$(cat .apm/instructions/python-rules.instructions.md)" == *"python-rules"* ]]

  # Pick one prompt
  run "$INSTILL_BIN" pick --type prompt debug
  [ "$status" -eq 0 ]
  [ -f .apm/prompts/debug.prompt.md ]
  [[ "$(cat .apm/prompts/debug.prompt.md)" == *"debug"* ]]

  # Sync exercises apm install + compile
  run "$INSTILL_BIN" sync
  [ "$status" -eq 0 ]
  [[ "$output" == *"ok: synced"* ]]

  # Verify all artifacts present
  [ -f apm.lock.yaml ]
  [ -d .apm ]
  [[ "$(cat apm.yml)" == *"$INSTILL_LIBRARY_PATH/skills/docker"* ]]
  [[ "$(cat apm.yml)" == *"name: local-db"* ]]
  [[ "$(cat apm.yml)" == *"name: project"* ]]
  [[ "$(cat apm.yml)" == *"version: 0.1.0"* ]]
}

# ──────────────────────────────────────────────────────────────────────────────
# E2E: import old-instill migrates legacy manifest and removes artifacts
# ──────────────────────────────────────────────────────────────────────────────

@test "import old-instill migrates legacy manifest and removes artifacts" {
  make_skill docker
  make_project
  scan_library

  # Set up legacy state: manifest + symlink + settings.local.json
  mkdir -p .claude/skills
  printf '{"skills":["docker"]}\n' > .claude/skill-manifest.json
  ln -s "$INSTILL_LIBRARY_PATH/skills/docker" .claude/skills/docker
  printf '{"permissions":{"allow":["Skill(docker)"]}}\n' > .claude/settings.local.json

  run "$INSTILL_BIN" import old-instill
  [ "$status" -eq 0 ]

  # Catalog updated with skill entry
  [ -f "$INSTILL_LIBRARY_PATH/skills/catalog.csv" ]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/skills/catalog.csv")" == *"docker"* ]]

  # apm.yml created with skill dependency
  [ -f apm.yml ]
  [[ "$(cat apm.yml)" == *"$INSTILL_LIBRARY_PATH/skills/docker"* ]]

  # Legacy artifacts removed
  [ ! -e .claude/skill-manifest.json ]
  [ ! -e .claude/skills/docker ]
  [ ! -e .claude/settings.local.json ]
}

# ──────────────────────────────────────────────────────────────────────────────
# E2E: import graft migrates mcp servers and removes graft.lock
# ──────────────────────────────────────────────────────────────────────────────

@test "import graft migrates mcp servers and removes graft.lock" {
  make_project

  # Set up graft state
  printf 'servers:\n  - local-db\n' > graft.lock
  printf '{"mcpServers":{"local-db":{"command":"sqlite-mcp","args":["--db","dev.db"]}}}\n' > .mcp.json

  run "$INSTILL_BIN" import graft
  [ "$status" -eq 0 ]

  # MCP catalog created in library
  [ -f "$INSTILL_LIBRARY_PATH/mcp/catalog.csv" ]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/catalog.csv")" == *"local-db"* ]]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/catalog.csv")" == *"sqlite-mcp"* ]]

  # Marker file created in library
  [ -f "$INSTILL_LIBRARY_PATH/mcp/local-db/config.json" ]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/local-db/config.json")" == *"sqlite-mcp"* ]]

  # apm.yml created with MCP dependency
  [ -f apm.yml ]
  [[ "$(cat apm.yml)" == *"name: local-db"* ]]
  [[ "$(cat apm.yml)" == *"transport: stdio"* ]]
  [[ "$(cat apm.yml)" == *"registry: false"* ]]
  [[ "$(cat apm.yml)" == *"command: sqlite-mcp"* ]]
  [[ "$(cat apm.yml)" == *"--db"* ]]

  # graft.lock removed
  [ ! -e graft.lock ]

  # .mcp.json removed (all servers imported)
  [ ! -e .mcp.json ]
}

# ──────────────────────────────────────────────────────────────────────────────
# E2E: import claude imports mcp servers with redacted env
# ──────────────────────────────────────────────────────────────────────────────

@test "import claude imports mcp servers from claude config with redacted env" {
  make_project

  # Use CLAUDE_CONFIG_DIR to provide a test fixture
  export CLAUDE_CONFIG_DIR="$BATS_TEST_TMPDIR/claude-config"
  mkdir -p "$CLAUDE_CONFIG_DIR"
  cat > "$CLAUDE_CONFIG_DIR/claude.json" <<'FIXTURE'
{
  "mcpServers": {
    "docs-search": {
      "command": "docs-mcp",
      "args": ["serve"],
      "env": {"API_KEY": "secret-key-123", "REGION": "us-east-1"}
    }
  },
  "projects": {
    "/tmp/example": {
      "mcpServers": {
        "project-db": {"command": "sqlite-mcp", "args": ["--db", "app.db"]}
      }
    }
  }
}
FIXTURE

  run "$INSTILL_BIN" import claude
  [ "$status" -eq 0 ]

  # MCP catalog created with both servers
  [ -f "$INSTILL_LIBRARY_PATH/mcp/catalog.csv" ]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/catalog.csv")" == *"docs-search"* ]]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/catalog.csv")" == *"project-db"* ]]

  # Env values are redacted (placeholder form, not actual secrets)
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/catalog.csv")" == *'${API_KEY}'* ]]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/catalog.csv")" == *'${REGION}'* ]]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/catalog.csv")" != *"secret-key-123"* ]]

  # Marker files created for each server
  [ -f "$INSTILL_LIBRARY_PATH/mcp/docs-search/config.json" ]
  [ -f "$INSTILL_LIBRARY_PATH/mcp/project-db/config.json" ]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/docs-search/config.json")" == *"docs-mcp"* ]]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/project-db/config.json")" == *"sqlite-mcp"* ]]

  # Does NOT create apm.yml (library-only operation)
  [ ! -e apm.yml ]
}

# ──────────────────────────────────────────────────────────────────────────────
# E2E: import directory copies typed content and rebuilds all catalogs
# ──────────────────────────────────────────────────────────────────────────────

@test "import directory copies typed content and rebuilds all catalogs" {
  make_project

  # Create a source directory with one of each type
  SOURCE="$BATS_TEST_TMPDIR/source"
  mkdir -p "$SOURCE/my-skill"
  printf '# my-skill\n' > "$SOURCE/my-skill/SKILL.md"

  mkdir -p "$SOURCE/my-mcp"
  printf '{"transport":"stdio","command":"my-mcp-server","args":["--port","3000"]}\n' > "$SOURCE/my-mcp/config.json"

  mkdir -p "$SOURCE/my-instruction"
  printf '# Follow these rules\n' > "$SOURCE/my-instruction/INSTRUCTION.md"

  mkdir -p "$SOURCE/my-prompt"
  printf '# Debug prompt\n' > "$SOURCE/my-prompt/PROMPT.md"

  run "$INSTILL_BIN" import directory "$SOURCE"
  [ "$status" -eq 0 ]

  # Content copied into library subdirectories
  [ -f "$INSTILL_LIBRARY_PATH/skills/my-skill/SKILL.md" ]
  [ -f "$INSTILL_LIBRARY_PATH/mcp/my-mcp/config.json" ]
  [ -f "$INSTILL_LIBRARY_PATH/instructions/my-instruction/INSTRUCTION.md" ]
  [ -f "$INSTILL_LIBRARY_PATH/prompts/my-prompt/PROMPT.md" ]

  # All four catalogs rebuilt
  [ -f "$INSTILL_LIBRARY_PATH/skills/catalog.csv" ]
  [ -f "$INSTILL_LIBRARY_PATH/mcp/catalog.csv" ]
  [ -f "$INSTILL_LIBRARY_PATH/instructions/catalog.csv" ]
  [ -f "$INSTILL_LIBRARY_PATH/prompts/catalog.csv" ]

  # Catalog contents reference the imported entries
  [[ "$(cat "$INSTILL_LIBRARY_PATH/skills/catalog.csv")" == *"my-skill"* ]]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/catalog.csv")" == *"my-mcp"* ]]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/mcp/catalog.csv")" == *"my-mcp-server"* ]]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/instructions/catalog.csv")" == *"my-instruction"* ]]
  [[ "$(cat "$INSTILL_LIBRARY_PATH/prompts/catalog.csv")" == *"my-prompt"* ]]

  # Does NOT create apm.yml (library-only operation)
  [ ! -e apm.yml ]
}

# ──────────────────────────────────────────────────────────────────────────────
# Multi-harness: sync produces artifacts for both .claude/ and .agents/
# ──────────────────────────────────────────────────────────────────────────────

@test "sync produces artifacts for both harnesses" {
  make_skill docker
  make_project
  scan_library

  run "$INSTILL_BIN" init --skills docker
  [ "$status" -eq 0 ]

  run "$INSTILL_BIN" sync
  [ "$status" -eq 0 ]

  assert_both_harnesses_installed
  assert_both_harnesses_compiled
}

# ──────────────────────────────────────────────────────────────────────────────
# Multi-harness: sync with only Claude harness present
# ──────────────────────────────────────────────────────────────────────────────

@test "sync works when only .claude/ harness exists" {
  make_skill docker
  make_project
  scan_library

  install_fake_apm_claude_only

  run "$INSTILL_BIN" init --skills docker
  [ "$status" -eq 0 ]

  run "$INSTILL_BIN" sync
  [ "$status" -eq 0 ]

  [ -f .claude/.apm-compiled ]
  [ ! -f .agents/.apm-installed ]
}

# ──────────────────────────────────────────────────────────────────────────────
# Multi-harness: import old-instill removes symlinks from both harness dirs
# ──────────────────────────────────────────────────────────────────────────────

@test "import old-instill removes symlinks from both harness directories" {
  make_skill docker
  make_project
  scan_library

  write_legacy_manifest '{"skills":["docker"]}'
  setup_dual_harness_legacy docker
  printf '{"permissions":{"allow":["Skill(docker)"]}}\n' > .claude/settings.local.json

  run "$INSTILL_BIN" import old-instill
  [ "$status" -eq 0 ]

  [ ! -e .claude/skills/docker ]
  [ ! -e .agents/skills/docker ]
  assert_no_legacy_symlinks .claude/skills
  assert_no_legacy_symlinks .agents/skills
}

# ──────────────────────────────────────────────────────────────────────────────
# Multi-harness: import handles missing .agents/skills gracefully
# ──────────────────────────────────────────────────────────────────────────────

@test "import old-instill handles missing .agents/skills gracefully" {
  make_skill docker
  make_project
  scan_library

  write_legacy_manifest '{"skills":["docker"]}'
  mkdir -p .claude/skills
  ln -s "$INSTILL_LIBRARY_PATH/skills/docker" .claude/skills/docker
  printf '{"permissions":{"allow":["Skill(docker)"]}}\n' > .claude/settings.local.json

  run "$INSTILL_BIN" import old-instill
  [ "$status" -eq 0 ]

  [ ! -e .claude/skills/docker ]
  [ ! -d .agents/skills ]
}

# ──────────────────────────────────────────────────────────────────────────────
# Multi-harness: init and pick work with both harness directories present
# ──────────────────────────────────────────────────────────────────────────────

@test "init and pick work with both harness directories present" {
  make_skill docker
  make_skill golang-testing
  make_mcp local-db
  make_project
  scan_library

  mkdir -p .claude .agents

  run "$INSTILL_BIN" init --skills docker
  [ "$status" -eq 0 ]
  [ -f apm.yml ]

  run "$INSTILL_BIN" pick --type skill golang-testing
  [ "$status" -eq 0 ]
  [[ "$(cat apm.yml)" == *"golang-testing"* ]]

  run "$INSTILL_BIN" pick --type mcp local-db
  [ "$status" -eq 0 ]
  [[ "$(cat apm.yml)" == *"local-db"* ]]

  run "$INSTILL_BIN" sync
  [ "$status" -eq 0 ]

  assert_both_harnesses_installed
  assert_both_harnesses_compiled
}

# ──────────────────────────────────────────────────────────────────────────────
# Targets: init writes targets when multiple harnesses detected
# ──────────────────────────────────────────────────────────────────────────────

@test "init writes targets when multiple harnesses detected" {
  make_skill docker
  make_project
  scan_library

  mkdir -p .claude .codex

  run "$INSTILL_BIN" init --skills docker
  [ "$status" -eq 0 ]
  [ -f apm.yml ]

  [[ "$(cat apm.yml)" == *"targets:"* ]]
  [[ "$(cat apm.yml)" == *"claude"* ]]
  [[ "$(cat apm.yml)" == *"codex"* ]]
}

# ──────────────────────────────────────────────────────────────────────────────
# Targets: sync adds targets to existing manifest missing them
# ──────────────────────────────────────────────────────────────────────────────

@test "sync adds targets to existing manifest missing them" {
  make_skill docker
  make_project
  scan_library

  mkdir -p .claude .codex

  printf 'name: project\nversion: 0.1.0\ndependencies:\n    apm:\n        - %s/skills/docker\n' "$INSTILL_LIBRARY_PATH" > apm.yml

  [[ "$(cat apm.yml)" != *"targets:"* ]]

  run "$INSTILL_BIN" sync
  [ "$status" -eq 0 ]

  [[ "$(cat apm.yml)" == *"targets:"* ]]
  [[ "$(cat apm.yml)" == *"claude"* ]]
  [[ "$(cat apm.yml)" == *"codex"* ]]
}

# ──────────────────────────────────────────────────────────────────────────────
# Targets: strict APM rejects missing targets, instill prevents failure
# ──────────────────────────────────────────────────────────────────────────────

@test "sync with strict APM succeeds because instill writes targets first" {
  make_skill docker
  make_project
  scan_library

  mkdir -p .claude .codex
  install_fake_apm_strict

  run "$INSTILL_BIN" init --skills docker
  [ "$status" -eq 0 ]

  run "$INSTILL_BIN" sync
  [ "$status" -eq 0 ]
  [[ "$(cat apm.yml)" == *"targets:"* ]]
}

# ──────────────────────────────────────────────────────────────────────────────
# Targets: init with explicit --targets sets targets in manifest
# ──────────────────────────────────────────────────────────────────────────────

@test "init with explicit targets flag sets targets in apm.yml" {
  make_skill docker
  make_project
  scan_library

  run "$INSTILL_BIN" init --targets codex,opencode,hermes,pi,claude,antigravity --skills docker
  [ "$status" -eq 0 ]
  [ -f apm.yml ]

  [[ "$(cat apm.yml)" == *"targets:"* ]]
  [[ "$(cat apm.yml)" == *"codex"* ]]
  [[ "$(cat apm.yml)" == *"opencode"* ]]
  [[ "$(cat apm.yml)" == *"hermes"* ]]
  [[ "$(cat apm.yml)" == *"pi"* ]]
  [[ "$(cat apm.yml)" == *"claude"* ]]
  [[ "$(cat apm.yml)" == *"antigravity"* ]]
}

# ──────────────────────────────────────────────────────────────────────────────
# Targets: targets command updates and lists targets
# ──────────────────────────────────────────────────────────────────────────────

@test "targets command updates targets in apm.yml and lists them" {
  make_skill docker
  make_project
  scan_library

  run "$INSTILL_BIN" init --targets claude --skills docker
  [ "$status" -eq 0 ]

  run "$INSTILL_BIN" targets codex opencode hermes
  [ "$status" -eq 0 ]
  [[ "$output" == *"ok: targets set to codex, opencode, hermes"* ]]

  [[ "$(cat apm.yml)" == *"codex"* ]]
  [[ "$(cat apm.yml)" == *"opencode"* ]]
  [[ "$(cat apm.yml)" == *"hermes"* ]]

  run "$INSTILL_BIN" targets
  [ "$status" -eq 0 ]
  [[ "$output" == *"codex"* ]]
  [[ "$output" == *"opencode"* ]]
  [[ "$output" == *"hermes"* ]]
}

@test "init with opencode target creates apm.yml targeting opencode" {
  make_skill docker
  make_project
  scan_library

  run "$INSTILL_BIN" init --targets opencode --skills docker
  [ "$status" -eq 0 ]
  [ -f apm.yml ]
  [[ "$(cat apm.yml)" == *"opencode"* ]]
}

@test "library add registers a repository-backed plugin from marketplace metadata" {
  install_fake_git_remote_plugin
  make_project

  run "$INSTILL_BIN" library add --type plugin --repository pbakaus/impeccable --name impeccable
  [ "$status" -eq 0 ]

  catalog="$(cat "$INSTILL_LIBRARY_PATH/plugins/catalog.csv")"
  [[ "$catalog" == *"impeccable,design,plugin,git,https://github.com/pbakaus/impeccable.git,3333333333333333333333333333333333333333,Design fluency"* ]]

  printf 'name: project\nversion: 0.1.0\ndependencies: {}\n' > apm.yml
  run "$INSTILL_BIN" pick --type plugin impeccable
  [ "$status" -eq 0 ]

  manifest="$(cat apm.yml)"
  [[ "$manifest" == *"git: https://github.com/pbakaus/impeccable.git"* ]]
  [[ "$manifest" == *"path: plugin"* ]]
  [[ "$manifest" == *"ref: \"3333333333333333333333333333333333333333\""* ]]
  [ -f apm.lock.yaml ]
}
