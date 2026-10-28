#!/usr/bin/env bash

make_project() {
  PROJECT="$BATS_TEST_TMPDIR/project"
  mkdir -p "$PROJECT"
  cd "$PROJECT"
}

make_skill() {
  mkdir -p "$INSTILL_LIBRARY_PATH/skills/$1"
  printf '# %s\n' "$1" > "$INSTILL_LIBRARY_PATH/skills/$1/SKILL.md"
}

add_skill_file() {
  mkdir -p "$INSTILL_LIBRARY_PATH/skills/$1/$(dirname "$2")"
  printf '#!/bin/sh\necho %s\n' "$2" > "$INSTILL_LIBRARY_PATH/skills/$1/$2"
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

install_fake_git_marketplace() {
  local format="${1:-claude}"
  export INSTILL_TEST_MARKETPLACE_FORMAT="$format"
  mkdir -p "$BATS_TEST_TMPDIR/bin"
  cat > "$BATS_TEST_TMPDIR/bin/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

sha="3333333333333333333333333333333333333333"
command="$*"
case "$command" in
  "ls-remote --symref https://github.com/mattpocock/skills.git HEAD")
    printf '%s\tHEAD\n' "$sha"
    ;;
  init\ *)
    mkdir -p "$2/.git"
    ;;
  *remote\ add\ origin\ https://github.com/mattpocock/skills.git)
    ;;
  *fetch\ --depth\ 1\ origin\ "$sha")
    ;;
  *checkout\ --force\ --detach\ "$sha")
    work_tree=""
    for arg in "$@"; do
      case "$arg" in
        --work-tree=*) work_tree="${arg#--work-tree=}" ;;
      esac
    done
    format="${INSTILL_TEST_MARKETPLACE_FORMAT:-claude}"
    case "$format" in
      claude)
        mkdir -p "$work_tree/.claude-plugin" "$work_tree/skills/tdd"
        printf '%s\n' '{"name":"mattpocock","plugins":[{"name":"mattpocock-skills","source":"./","description":"Matt Pocock skills"}]}' > "$work_tree/.claude-plugin/marketplace.json"
        printf '%s\n' '{"name":"mattpocock-skills"}' > "$work_tree/.claude-plugin/plugin.json"
        printf '# tdd\n' > "$work_tree/skills/tdd/SKILL.md"
        ;;
      codex)
        mkdir -p "$work_tree/.agents/plugins" "$work_tree/plugins/stack/.codex-plugin"
        printf '%s\n' '{"name":"stack","plugins":[{"name":"stack","source":{"source":"local","path":"./plugins/stack"}}]}' > "$work_tree/.agents/plugins/marketplace.json"
        printf '%s\n' '{"name":"stack","description":"Stack plugin"}' > "$work_tree/plugins/stack/.codex-plugin/plugin.json"
        ;;
      *)
        printf 'unsupported fake marketplace format: %s\n' "$format" >&2
        exit 1
        ;;
    esac
    ;;
  *)
    printf 'unexpected git command: %s\n' "$command" >&2
    exit 1
    ;;
esac
EOF
  chmod +x "$BATS_TEST_TMPDIR/bin/git"
}

write_legacy_manifest() {
  mkdir -p .claude/skills
  printf '%s\n' "$1" > .claude/skill-manifest.json
}
