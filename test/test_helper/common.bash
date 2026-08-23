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

install_fake_git_remote_plugin() {
  mkdir -p "$BATS_TEST_TMPDIR/bin"
  cat > "$BATS_TEST_TMPDIR/bin/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

sha="3333333333333333333333333333333333333333"
command="$*"
case "$command" in
  "ls-remote --symref https://github.com/pbakaus/impeccable.git HEAD")
    printf '%s\tHEAD\n' "$sha"
    ;;
  init\ *)
    mkdir -p "$2/.git"
    ;;
  -C\ *\ remote\ add\ origin\ https://github.com/pbakaus/impeccable.git)
    ;;
  -C\ *\ fetch\ --depth\ 1\ origin\ "$sha")
    ;;
  -C\ *\ ls-tree\ "$sha"\ --\ .claude-plugin/marketplace.json)
    printf '100644 blob abc\t.claude-plugin/marketplace.json\n'
    ;;
  -C\ *\ cat-file\ -s\ "$sha":.claude-plugin/marketplace.json)
    printf '130\n'
    ;;
  -C\ *\ show\ "$sha":.claude-plugin/marketplace.json)
    printf '%s\n' '{"plugins":[{"name":"impeccable","description":"Design fluency","category":"design","source":"./plugin"}]}'
    ;;
  -C\ *\ ls-tree\ "$sha"\ --\ plugin/.claude-plugin/plugin.json)
    printf '100644 blob def\tplugin/.claude-plugin/plugin.json\n'
    ;;
  -C\ *\ cat-file\ -s\ "$sha":plugin/.claude-plugin/plugin.json)
    printf '21\n'
    ;;
  -C\ *\ ls-tree\ "$sha"\ --\ plugin)
    printf '040000 tree def\tplugin\n'
    ;;
  -C\ *\ show\ "$sha":plugin/.claude-plugin/plugin.json)
    printf '%s\n' '{"name":"impeccable"}'
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
