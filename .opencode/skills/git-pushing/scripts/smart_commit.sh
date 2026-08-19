#!/usr/bin/env bash
# Smart Git Commit Script for git-pushing skill
# Modes: push (default) | --pr | --full | --release
# Options: --tag <tag> | --title <title> | --notes <notes> | --notes-file <file> | --draft | --prerelease
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { printf "%b→%b %s\n" "${GREEN}" "${NC}" "$*"; }
warn() { printf "%b⚠%b %s\n" "${YELLOW}" "${NC}" "$*"; }
error() { printf "%b✗%b %s\n" "${RED}" "${NC}" "$*" >&2; }

# --- Parse mode + options + message ---
MODE="push"
COMMIT_MSG=""
TAG_NAME=""
RELEASE_TITLE=""
RELEASE_NOTES=""
RELEASE_NOTES_FILE=""
IS_DRAFT=false
IS_PRERELEASE=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pr)
      MODE="pr"
      shift
      ;;
    --full)
      MODE="full"
      shift
      ;;
    --release)
      MODE="release"
      shift
      if [[ $# -gt 0 && ! "$1" =~ ^-- ]]; then
        TAG_NAME="$1"
        shift
      fi
      ;;
    --tag|-t)
      if [[ $# -lt 2 ]]; then
        error "Flag $1 requires a tag name argument"
        exit 1
      fi
      TAG_NAME="$2"
      shift 2
      ;;
    --tag=*|-t=*)
      TAG_NAME="${1#*=}"
      shift
      ;;
    --title)
      if [[ $# -lt 2 ]]; then
        error "Flag $1 requires a title argument"
        exit 1
      fi
      RELEASE_TITLE="$2"
      shift 2
      ;;
    --title=*)
      RELEASE_TITLE="${1#*=}"
      shift
      ;;
    --notes|-n)
      if [[ $# -lt 2 ]]; then
        error "Flag $1 requires a notes argument"
        exit 1
      fi
      RELEASE_NOTES="$2"
      shift 2
      ;;
    --notes=*)
      RELEASE_NOTES="${1#*=}"
      shift
      ;;
    --notes-file|-F)
      if [[ $# -lt 2 ]]; then
        error "Flag $1 requires a file path argument"
        exit 1
      fi
      RELEASE_NOTES_FILE="$2"
      shift 2
      ;;
    --notes-file=*)
      RELEASE_NOTES_FILE="${1#*=}"
      shift
      ;;
    --draft|-d)
      IS_DRAFT=true
      shift
      ;;
    --prerelease|-p)
      IS_PRERELEASE=true
      shift
      ;;
    *)
      if [[ -z "$COMMIT_MSG" ]]; then
        COMMIT_MSG="$1"
      else
        COMMIT_MSG="$COMMIT_MSG $1"
      fi
      shift
      ;;
  esac
done

info "Mode: $MODE"
if [[ -n "$TAG_NAME" ]]; then
  info "Tag/Release: $TAG_NAME"
fi

# Co-author identity — CLI_TOOL_NAME/CLI_MODEL_NAME let other CLI tools (Codex, OpenCode, etc.) override
CLI_TOOL_NAME="${CLI_TOOL_NAME:-${HARNESS_NAME:-OpenCode}}"
CLI_MODEL_NAME="${CLI_MODEL_NAME:-gemini-3.7-flash}"
CO_AUTHORS="Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: ${CLI_TOOL_NAME} <noreply@anthropic.com> - ${CLI_MODEL_NAME}"

CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
info "Current branch: $CURRENT_BRANCH"

# Check if there are uncommitted changes
HAS_CHANGES=false
if ! git diff --quiet || ! git diff --cached --quiet || [[ -n "$(git status --porcelain)" ]]; then
  HAS_CHANGES=true
fi

# --- Auto-create a feature branch if on main/master and changes exist ---
# Peter has explicitly corrected this before ("always make a feature branch") —
# never commit straight to main/master regardless of mode.
if [[ "$CURRENT_BRANCH" == "main" || "$CURRENT_BRANCH" == "master" ]] && [[ "$HAS_CHANGES" == "true" ]]; then
  STAGED_FOR_NAME=$(git status --porcelain | awk '{print $2}')
  if echo "$STAGED_FOR_NAME" | grep -qE "\.(md|txt|rst)$"; then
    PREFIX="docs"
  elif echo "$STAGED_FOR_NAME" | grep -qE "test"; then
    PREFIX="test"
  elif [[ -n "$TAG_NAME" ]]; then
    PREFIX="release"
  else
    PREFIX="feat"
  fi
  BRANCH_NAME="${PREFIX}/$(date +%Y%m%d)-update"
  info "On $CURRENT_BRANCH with changes — creating feature branch $BRANCH_NAME"
  git checkout -b "$BRANCH_NAME"
  CURRENT_BRANCH="$BRANCH_NAME"
fi

handle_tag_and_release() {
  local tag="$1"
  local notes="$2"
  local notes_file="$3"
  local title="$4"
  local is_draft="$5"
  local is_prerelease="$6"

  if [[ -z "$tag" ]]; then
    return 0
  fi

  info "Tagging HEAD as $tag..."
  if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
    warn "Git tag '$tag' already exists locally."
  else
    local tag_msg
    if [[ -n "$notes" ]]; then
      tag_msg="$notes"
    elif [[ -n "$title" ]]; then
      tag_msg="$title"
    else
      tag_msg="Release $tag"
    fi
    git tag -a "$tag" -m "$tag_msg"
    info "Created git tag: $tag"
  fi

  info "Pushing tag $tag to origin..."
  if git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null 2>&1; then
    warn "Tag $tag already exists on remote origin."
  else
    git push origin "$tag"
    info "Successfully pushed tag $tag to origin"
  fi

  # GitHub Release creation if gh is available and remote is GitHub
  local remote_url
  remote_url=$(git remote get-url origin 2>/dev/null || echo "")
  if echo "$remote_url" | grep -q "github.com" && command -v gh >/dev/null 2>&1; then
    info "Creating GitHub Release for $tag..."
    local release_args=("$tag")

    if [[ -n "$title" ]]; then
      release_args+=("--title" "$title")
    fi

    if [[ -n "$notes" ]]; then
      release_args+=("--notes" "$notes")
    elif [[ -n "$notes_file" ]]; then
      release_args+=("--notes-file" "$notes_file")
    else
      release_args+=("--generate-notes")
    fi

    if [[ "$is_draft" == "true" ]]; then
      release_args+=("--draft")
    fi

    if [[ "$is_prerelease" == "true" ]]; then
      release_args+=("--prerelease")
    fi

    if gh release view "$tag" >/dev/null 2>&1; then
      warn "GitHub Release $tag already exists"
      local existing_url
      existing_url=$(gh release view "$tag" --json url -q .url 2>/dev/null || echo "")
      if [[ -n "$existing_url" ]]; then
        info "Existing Release URL: $existing_url"
      fi
    else
      local release_url
      release_url=$(gh release create "${release_args[@]}" 2>&1 | tail -1)
      info "GitHub Release: $release_url"
    fi
  else
    warn "gh CLI not found or remote not GitHub — skipped GitHub Release creation (git tag was pushed)."
  fi
}

determine_commit_type() {
  local files="$1"
  if echo "$files" | grep -q "test"; then
    echo "test"
  elif echo "$files" | grep -qE "\.(md|txt|rst)$"; then
    echo "docs"
  elif echo "$files" | grep -qE "package\.json|requirements\.txt|Cargo\.toml|go\.mod|mise\.toml"; then
    echo "chore"
  elif git diff --cached | grep -qE "^[\+].*fix|^[\+].*bug"; then
    echo "fix"
  elif git diff --cached | grep -qE "^[\+].*refactor"; then
    echo "refactor"
  elif [[ -n "$TAG_NAME" ]]; then
    echo "chore"
  else
    echo "feat"
  fi
}

determine_scope() {
  local files="$1"
  local scope
  scope=$(echo "$files" | head -1 | cut -d'/' -f1)
  if echo "$files" | grep -q "plugin"; then
    echo "plugin"
  elif echo "$files" | grep -q "skill"; then
    echo "skill"
  elif echo "$files" | grep -q "agent"; then
    echo "agent"
  elif [[ -n "$TAG_NAME" ]]; then
    echo "release"
  elif [[ -n "$scope" && "$scope" != "." ]]; then
    echo "$scope"
  else
    echo ""
  fi
}

if [[ "$HAS_CHANGES" == "true" ]]; then
  info "Staging all changes..."
  git add .

  STAGED_FILES=$(git diff --cached --name-only)
  DIFF_STAT=$(git diff --cached --stat)

  if [[ -z "$COMMIT_MSG" ]]; then
    COMMIT_TYPE=$(determine_commit_type "$STAGED_FILES")
    SCOPE=$(determine_scope "$STAGED_FILES")
    NUM_FILES=$(echo "$STAGED_FILES" | wc -l | xargs)

    if [[ "$COMMIT_TYPE" == "docs" ]]; then
      DESCRIPTION="update documentation"
    elif [[ "$COMMIT_TYPE" == "test" ]]; then
      DESCRIPTION="update tests"
    elif [[ "$COMMIT_TYPE" == "chore" ]]; then
      DESCRIPTION="update dependencies"
    elif [[ -n "$TAG_NAME" ]]; then
      DESCRIPTION="prepare release $TAG_NAME"
    else
      DESCRIPTION="update $NUM_FILES file(s)"
    fi

    if [[ -n "$SCOPE" ]]; then
      COMMIT_MSG="${COMMIT_TYPE}(${SCOPE}): ${DESCRIPTION}"
    else
      COMMIT_MSG="${COMMIT_TYPE}: ${DESCRIPTION}"
    fi
    info "Generated commit message: $COMMIT_MSG"
  else
    info "Using provided message: $COMMIT_MSG"
  fi

  git commit -m "$(cat <<EOF
${COMMIT_MSG}

${CO_AUTHORS}
EOF
)"

  COMMIT_HASH=$(git rev-parse --short HEAD)
  info "Created commit: $COMMIT_HASH"

  info "Pushing to origin/$CURRENT_BRANCH..."
  if git ls-remote --exit-code --heads origin "$CURRENT_BRANCH" >/dev/null 2>&1; then
    git push
  else
    git push -u origin "$CURRENT_BRANCH"
  fi
  info "Successfully pushed to origin/$CURRENT_BRANCH"
  echo "$DIFF_STAT"
else
  info "No uncommitted changes."
  if [[ -z "$TAG_NAME" && "$MODE" != "full" && "$MODE" != "release" ]]; then
    warn "No changes to commit and no tag specified."
    exit 0
  fi
fi

if [[ "$MODE" == "push" || "$MODE" == "release" ]]; then
  if [[ -n "$TAG_NAME" ]]; then
    handle_tag_and_release "$TAG_NAME" "$RELEASE_NOTES" "$RELEASE_NOTES_FILE" "$RELEASE_TITLE" "$IS_DRAFT" "$IS_PRERELEASE"
  fi
  exit 0
fi

# --- pr / full modes need gh + a GitHub remote ---
REMOTE_URL=$(git remote get-url origin 2>/dev/null || echo "")
if ! echo "$REMOTE_URL" | grep -q "github.com"; then
  warn "Remote is not GitHub — stopping after push. Open the PR manually."
  if [[ -n "$TAG_NAME" ]]; then
    handle_tag_and_release "$TAG_NAME" "$RELEASE_NOTES" "$RELEASE_NOTES_FILE" "$RELEASE_TITLE" "$IS_DRAFT" "$IS_PRERELEASE"
  fi
  exit 0
fi
if ! command -v gh >/dev/null 2>&1; then
  warn "gh CLI not found — stopping after push. Run 'gh pr create' manually, or install gh."
  if [[ -n "$TAG_NAME" ]]; then
    handle_tag_and_release "$TAG_NAME" "$RELEASE_NOTES" "$RELEASE_NOTES_FILE" "$RELEASE_TITLE" "$IS_DRAFT" "$IS_PRERELEASE"
  fi
  exit 0
fi

DEFAULT_BRANCH=$(gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null || echo "main")

# If on default branch with no pending branch/PR, handle tag & release directly
if [[ "$CURRENT_BRANCH" == "$DEFAULT_BRANCH" && "$HAS_CHANGES" == "false" ]]; then
  info "Already on $DEFAULT_BRANCH with no pending branch changes."
  if [[ -n "$TAG_NAME" ]]; then
    handle_tag_and_release "$TAG_NAME" "$RELEASE_NOTES" "$RELEASE_NOTES_FILE" "$RELEASE_TITLE" "$IS_DRAFT" "$IS_PRERELEASE"
  fi
  exit 0
fi

info "Opening PR..."
PR_URL=$(gh pr create --fill --head "$CURRENT_BRANCH" 2>&1 | tail -1)
info "PR: $PR_URL"

if [[ "$MODE" == "pr" ]]; then
  if [[ -n "$TAG_NAME" ]]; then
    warn "PR opened. Tag '$TAG_NAME' not created yet — use --full mode to merge to $DEFAULT_BRANCH, tag, and release."
  fi
  exit 0
fi

# --- full mode: merge, return to main, pull, tag/release, delete branch ---
info "Merging PR..."
gh pr merge --merge --delete-branch=false "$CURRENT_BRANCH"

info "Returning to $DEFAULT_BRANCH..."
git checkout "$DEFAULT_BRANCH"
git pull

# Create tag and GitHub release on default branch after merge
if [[ -n "$TAG_NAME" ]]; then
  handle_tag_and_release "$TAG_NAME" "$RELEASE_NOTES" "$RELEASE_NOTES_FILE" "$RELEASE_TITLE" "$IS_DRAFT" "$IS_PRERELEASE"
fi

info "Deleting feature branch $CURRENT_BRANCH (local + remote)..."
git branch -d "$CURRENT_BRANCH" 2>/dev/null || git branch -D "$CURRENT_BRANCH"
git push origin --delete "$CURRENT_BRANCH" 2>/dev/null || warn "Remote branch already gone"

if [[ -n "$TAG_NAME" ]]; then
  info "Done — merged, tagged $TAG_NAME, released, back on $DEFAULT_BRANCH, pulled, feature branch removed."
else
  info "Done — merged, back on $DEFAULT_BRANCH, pulled, feature branch removed."
fi
exit 0
