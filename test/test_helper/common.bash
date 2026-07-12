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
