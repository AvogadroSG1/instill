#!/bin/bash
# Smart Git Commit Script for git-pushing skill
# Modes: push (default) | --pr | --full
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}→${NC} $1"; }
warn() { echo -e "${YELLOW}⚠${NC} $1"; }
error() { echo -e "${RED}✗${NC} $1" >&2; }

# --- Parse mode + message ---
MODE="push"
COMMIT_MSG=""
for arg in "$@"; do
    case "$arg" in
        --pr) MODE="pr" ;;
        --full) MODE="full" ;;
        *) COMMIT_MSG="$arg" ;;
    esac
done
info "Mode: $MODE"

# Co-author identity — CLI_TOOL_NAME/CLI_MODEL_NAME let other CLI tools (Codex, etc.) override
CLI_TOOL_NAME="${CLI_TOOL_NAME:-Claude Code}"
CLI_MODEL_NAME="${CLI_MODEL_NAME:-Claude}"
CO_AUTHORS="Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: ${CLI_MODEL_NAME} <noreply@anthropic.com> - ${CLI_TOOL_NAME}"

CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
info "Current branch: $CURRENT_BRANCH"

# --- Auto-create a feature branch if on main/master ---
# Peter has explicitly corrected this before ("always make a feature branch") —
# never commit straight to main/master regardless of mode.
if [ "$CURRENT_BRANCH" = "main" ] || [ "$CURRENT_BRANCH" = "master" ]; then
    STAGED_FOR_NAME=$(git status --porcelain | awk '{print $2}')
    if echo "$STAGED_FOR_NAME" | grep -qE "\.(md|txt|rst)$"; then
        PREFIX="docs"
    elif echo "$STAGED_FOR_NAME" | grep -qE "test"; then
        PREFIX="test"
    else
        PREFIX="feat"
    fi
    BRANCH_NAME="${PREFIX}/$(date +%Y%m%d)-update"
    info "On $CURRENT_BRANCH — creating feature branch $BRANCH_NAME"
    git checkout -b "$BRANCH_NAME"
    CURRENT_BRANCH="$BRANCH_NAME"
fi

if git diff --quiet && git diff --cached --quiet; then
    warn "No changes to commit"
    exit 0
fi

info "Staging all changes..."
git add .

STAGED_FILES=$(git diff --cached --name-only)
DIFF_STAT=$(git diff --cached --stat)

determine_commit_type() {
    local files="$1"
    if echo "$files" | grep -q "test"; then
        echo "test"
    elif echo "$files" | grep -qE "\.(md|txt|rst)$"; then
        echo "docs"
    elif echo "$files" | grep -qE "package\.json|requirements\.txt|Cargo\.toml"; then
        echo "chore"
    elif git diff --cached | grep -qE "^[\+].*fix|^[\+].*bug"; then
        echo "fix"
    elif git diff --cached | grep -qE "^[\+].*refactor"; then
        echo "refactor"
    else
        echo "feat"
    fi
}

determine_scope() {
    local files="$1"
    local scope=$(echo "$files" | head -1 | cut -d'/' -f1)
    if echo "$files" | grep -q "plugin"; then
        echo "plugin"
    elif echo "$files" | grep -q "skill"; then
        echo "skill"
    elif echo "$files" | grep -q "agent"; then
        echo "agent"
    elif [ -n "$scope" ] && [ "$scope" != "." ]; then
        echo "$scope"
    else
        echo ""
    fi
}

if [ -z "$COMMIT_MSG" ]; then
    COMMIT_TYPE=$(determine_commit_type "$STAGED_FILES")
    SCOPE=$(determine_scope "$STAGED_FILES")
    NUM_FILES=$(echo "$STAGED_FILES" | wc -l | xargs)

    if [ "$COMMIT_TYPE" = "docs" ]; then
        DESCRIPTION="update documentation"
    elif [ "$COMMIT_TYPE" = "test" ]; then
        DESCRIPTION="update tests"
    elif [ "$COMMIT_TYPE" = "chore" ]; then
        DESCRIPTION="update dependencies"
    else
        DESCRIPTION="update $NUM_FILES file(s)"
    fi

    if [ -n "$SCOPE" ]; then
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

if [ "$MODE" = "push" ]; then
    exit 0
fi

# --- pr / full modes need gh + a GitHub remote ---
REMOTE_URL=$(git remote get-url origin)
if ! echo "$REMOTE_URL" | grep -q "github.com"; then
    warn "Remote is not GitHub — stopping after push. Open the PR manually."
    exit 0
fi
if ! command -v gh >/dev/null 2>&1; then
    warn "gh CLI not found — stopping after push. Run 'gh pr create' manually, or install gh."
    exit 0
fi

info "Opening PR..."
PR_URL=$(gh pr create --fill --head "$CURRENT_BRANCH" 2>&1 | tail -1)
info "PR: $PR_URL"

if [ "$MODE" = "pr" ]; then
    exit 0
fi

# --- full mode: merge, return to main, pull, delete branch ---
DEFAULT_BRANCH=$(gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null || echo "main")

info "Merging PR..."
gh pr merge --merge --delete-branch=false "$CURRENT_BRANCH"

info "Returning to $DEFAULT_BRANCH..."
git checkout "$DEFAULT_BRANCH"
git pull

info "Deleting feature branch $CURRENT_BRANCH (local + remote)..."
git branch -d "$CURRENT_BRANCH" 2>/dev/null || git branch -D "$CURRENT_BRANCH"
git push origin --delete "$CURRENT_BRANCH" 2>/dev/null || warn "Remote branch already gone"

info "Done — merged, back on $DEFAULT_BRANCH, pulled, feature branch removed."
exit 0
