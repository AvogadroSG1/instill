#!/usr/bin/env bash
set -euo pipefail

SECRET_PATTERNS=(
  ".env"
  ".env."
  ".pem"
  ".key"
  "id_rsa"
  "id_ed25519"
  "credentials"
  "secret"
  ".tfstate"
  ".ssh/"
  ".aws/"
  ".gnupg/"
)

EXEMPT_PATTERNS=(
  "*.example"
  "*.sample"
  "*.template"
  ".claude/hooks/*"
)

usage() {
  echo "usage: $0 scan-command [--command <line>] | scan-staged" >&2
}

trim() {
  local value="${1-}"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s\n' "$value"
}

normalize_token() {
  local token="${1-}"

  while :; do
    case "$token" in
      "'"*|\"*|\(*|\[* )
        token="${token#?}"
        ;;
      *)
        break
        ;;
    esac
  done

  while :; do
    case "$token" in
      *"'"|*\"|*\)|*\])
        token="${token%?}"
        ;;
      *,|*\;)
        token="${token%?}"
        ;;
      *)
        break
        ;;
    esac
  done

  printf '%s\n' "$token"
}

is_exempt() {
  local token pattern
  token="$(normalize_token "$1")"

  for pattern in "${EXEMPT_PATTERNS[@]}"; do
    case "$token" in
      $pattern)
        return 0
        ;;
    esac
  done

  return 1
}

matches_secret() {
  local token pattern
  token="$(normalize_token "$1")"

  if [ -z "$token" ] || is_exempt "$token"; then
    return 1
  fi

  for pattern in "${SECRET_PATTERNS[@]}"; do
    case "$token" in
      *"$pattern"*)
        return 0
        ;;
    esac
  done

  return 1
}

is_filtered_env_dump() {
  [ "$(trim "$1")" = "env | grep -v SECRET" ]
}

command_basename() {
  local command_line first_token
  command_line="$(trim "$1")"
  [ -n "$command_line" ] || return 0
  first_token="${command_line%%[[:space:]]*}"
  first_token="$(normalize_token "$first_token")"
  basename "$first_token"
}

contains_proc_environ() {
  local command_line="$1" token
  local tokens=()
  read -r -a tokens <<<"$command_line" || true

  for token in "${tokens[@]}"; do
    token="$(normalize_token "$token")"
    case "$token" in
      /proc/*/environ)
        return 0
        ;;
    esac
  done

  return 1
}

is_launchctl_getenv() {
  local command_line="$1"
  local tokens=()
  read -r -a tokens <<<"$command_line" || true
  [ "${#tokens[@]}" -ge 2 ] || return 1
  [ "$(command_basename "$command_line")" = "launchctl" ] || return 1
  [ "$(normalize_token "${tokens[1]}")" = "getenv" ]
}

is_env_dump() {
  local command_line first_token_name
  command_line="$(trim "$1")"

  if [ -z "$command_line" ] || is_filtered_env_dump "$command_line"; then
    return 1
  fi

  if contains_proc_environ "$command_line" || is_launchctl_getenv "$command_line"; then
    return 0
  fi

  first_token_name="$(command_basename "$command_line")"
  case "$command_line" in
    set|export\ -p|declare\ -p|declare\ -x|declare\ -xp|declare\ -px|typeset\ -p|typeset\ -x|typeset\ -xp|typeset\ -px|compgen\ -v)
      return 0
      ;;
  esac

  case "$first_token_name" in
    env|printenv)
      return 0
      ;;
  esac

  return 1
}

read_command_from_stdin() {
  local stdin_payload=""

  if [ ! -t 0 ]; then
    stdin_payload="$(cat)"
  fi

  if [ -z "$stdin_payload" ]; then
    return 0
  fi

  if command -v jq >/dev/null 2>&1; then
    jq -r '.tool_input.command // .command // empty' 2>/dev/null <<<"$stdin_payload" || true
    return 0
  fi

  printf '%s\n' "$stdin_payload" | sed -n 's/.*"command"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p'
}

scan_command() {
  local command_line="" token
  local tokens=()

  if [ "${1:-}" = "--command" ]; then
    if [ "$#" -lt 2 ]; then
      usage
      return 64
    fi
    command_line="$2"
  elif [ "$#" -gt 0 ]; then
    usage
    return 64
  else
    command_line="$(read_command_from_stdin)"
  fi

  command_line="$(trim "$command_line")"
  if [ -z "$command_line" ]; then
    return 0
  fi

  if is_env_dump "$command_line"; then
    echo "BLOCKED [D10]: bare environment dump command '$command_line'" >&2
    return 2
  fi

  read -r -a tokens <<<"$command_line" || true
  for token in "${tokens[@]}"; do
    if matches_secret "$token"; then
      echo "BLOCKED [D9]: command references secret path '$token'" >&2
      return 2
    fi
  done

  return 0
}

scan_staged() {
  local staged_path
  local blocked_paths=()

  if [ "$#" -gt 0 ]; then
    usage
    return 64
  fi

  if ! git rev-parse --git-dir >/dev/null 2>&1; then
    return 0
  fi

  while IFS= read -r staged_path; do
    staged_path="$(trim "$staged_path")"
    if [ -z "$staged_path" ]; then
      continue
    fi

    if matches_secret "$staged_path"; then
      blocked_paths+=("$staged_path")
    fi
  done < <(git diff --cached --name-only 2>/dev/null || true)

  if [ "${#blocked_paths[@]}" -eq 0 ]; then
    return 0
  fi

  echo "BLOCKED [D9]: staged secret paths detected:" >&2
  for staged_path in "${blocked_paths[@]}"; do
    echo " - $staged_path" >&2
  done
  return 2
}

main() {
  local mode="${1:-}"
  case "$mode" in
    scan-command)
      shift
      scan_command "$@"
      ;;
    scan-staged)
      shift
      scan_staged "$@"
      ;;
    *)
      usage
      return 64
      ;;
  esac
}

main "$@"
