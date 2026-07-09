#!/usr/bin/env bash

assert_both_harnesses_installed() {
  [ -f .claude/.apm-installed ]
  [ -f .agents/.apm-installed ]
}

assert_both_harnesses_compiled() {
  [ -f .claude/.apm-compiled ]
  [ -f .agents/.apm-compiled ]
}

assert_no_legacy_symlinks() {
  local dir="$1"
  if [ -d "$dir" ]; then
    local count
    count="$(find "$dir" -type l 2>/dev/null | wc -l)"
    [ "$count" -eq 0 ]
  fi
}

setup_dual_harness_legacy() {
  local skill="$1"
  mkdir -p .claude/skills
  ln -s "$INSTILL_LIBRARY_PATH/skills/$skill" ".claude/skills/$skill"
  mkdir -p .agents/skills
  ln -s "$INSTILL_LIBRARY_PATH/skills/$skill" ".agents/skills/$skill"
}
