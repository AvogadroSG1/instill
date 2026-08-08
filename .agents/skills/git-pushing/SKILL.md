---
name: git-pushing
description: Run Peter's git workflows — commit+push, or the full branch → PR → merge → return-to-main → pull → delete-branch cycle. Use whenever Peter asks to push, commit, merge, PR, or finish up a branch, including phrases like "commit and push", "push this", "let's push these commits", "merge into main", "PR and merge", "go ahead commit push and merge", "did you push?", "did you remove the feature branch?", or invocations like "/git-pushing", "/git-pushing push", "/git-pushing pr". Always use this instead of running raw git commands one-by-one when the request matches these patterns.
---

# Git Push Workflow

Peter has asked for this same handful of git sequences hundreds of times over the past several months, worded differently each time ("commit and push", "PR and merge into main and return to main", "did you remove the feature branch?", "go ahead commit, push to feature branch, merge through pr, return to main, and pull"). The point of this skill is to stop making him spell out the steps — recognize which of the three modes below he means and just run it.

## The three modes

Figure out which mode applies from the wording. Default to **push** when in doubt — it's the smallest, safest action and matches how Peter phrases most requests ("commit and push", "let's push these commits"). Only escalate to `pr` or `full` when he actually says something about a PR, merge, or main.

| Mode | Trigger phrases | What it does |
|------|------------------|--------------|
| `push` (default) | "commit and push", "push this", "save this to github", "/git-pushing push", "/git-pushing" with no other detail | Branch (if needed) → stage → commit → push. Stops there. |
| `pr` | "PR this", "open a PR", "/git-pushing pr", "commit and push and PR" | Everything in `push`, then opens the PR. Does not merge. |
| `full` | "merge into main", "PR and merge", "go ahead commit push and merge", "return to main", "commit, push, PR, merge, return to main and pull", "delete the feature branch" | Everything in `pr`, then merges, switches back to main, pulls, and deletes the feature branch. |

Peter's actual phrasing is rarely this clean — he mixes and matches ("commit and push, merge to main through pr and return to main", "2 and also merge the PR into main, switch locally to main, pull and delete old feature branch"). Read the whole request for the *furthest* step he mentions (merge/main/delete-branch beats PR beats push) and run everything up to and including it.

## Why branching is non-negotiable

Peter has explicitly corrected this before ("Always make a feature branch" / "why don't you just make a feature branch"). **If the current branch is `main` or `master`, always create a feature branch before committing** — never commit straight to main, even in `push` mode. Name it descriptively based on the change (`feat/...`, `fix/...`, `docs/...`), matching the conventional-commit type you're about to use. If already on a non-main feature branch, just use it.

## Running it

```bash
PROJECT_ROOT=$(git rev-parse --show-toplevel)
bash "$PROJECT_ROOT/.agents/skills/git-pushing/scripts/smart_commit.sh"                # push mode, auto-generated message
bash "$PROJECT_ROOT/.agents/skills/git-pushing/scripts/smart_commit.sh" "feat: message" # push mode, explicit message
bash "$PROJECT_ROOT/.agents/skills/git-pushing/scripts/smart_commit.sh" --pr            # pr mode
bash "$PROJECT_ROOT/.agents/skills/git-pushing/scripts/smart_commit.sh" --pr "feat: message"
bash "$PROJECT_ROOT/.agents/skills/git-pushing/scripts/smart_commit.sh" --full          # full mode: PR + merge + return to main + pull + delete branch
bash "$PROJECT_ROOT/.agents/skills/git-pushing/scripts/smart_commit.sh" --full "feat: message"
```

The script handles:
- Creating a feature branch if you're on main/master
- Staging, generating a conventional commit message from the diff (or using the one you pass in)
- Committing with **both** required co-authors (see below — this is not optional, it's a hard requirement from Peter's CLAUDE.md)
- Pushing (`-u` for new branches)
- `--pr`: opening the PR via `gh pr create` and printing the URL
- `--full`: merging the PR, switching back to main, pulling, and deleting the local + remote feature branch

If `gh` isn't available or the repo has no GitHub remote, fall back to the manual path below for the PR/merge steps and tell Peter why.

## Manual path (script unavailable or custom situation)

**1. Check status and branch** — `git status`, `git branch --show-current`. If on main/master, create and switch to a feature branch first.

**2. Stage** — `git add .` (or specific files for a partial commit).

**3. Commit message** — conventional format `type(scope): description`, imperative mood, types `feat|fix|refactor|docs|test|chore`. If Peter gave a message, use it verbatim.

**4. Co-authors — required, not optional.** Peter's global instructions (CLAUDE.md) require every commit to carry both co-authors. Don't use the generic Claude Code footer alone — this has been a recurring correction point:

```bash
git commit -m "$(cat <<'EOF'
type(scope): description

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

(Swap the tool name/model if you're not Claude Code — e.g. `Codex <noreply@openai.com>`.)

**5. Push** — `git push`, or `git push -u origin <branch>` for a new branch.

**6. If `pr` or `full`:** open the PR (`gh pr create --title ... --body ...`), listing Peter and the CLI tool as co-authors in the body if the platform doesn't already carry commit co-authors through. Share the PR URL.

**7. If `full`:** once the PR is mergeable, merge it (`gh pr merge --merge` or per repo convention), `git checkout main`, `git pull`, then delete the feature branch both locally (`git branch -d <branch>`) and on remote (`git push origin --delete <branch>`) — this last step is easy to forget and Peter has had to ask "did you remove the feature branch?" more than once. Don't skip it.

**8. Confirm** — report the commit hash, PR link (if any), and final branch/sync state so Peter doesn't have to ask "did you push?" / "is this merged?" afterward.

## Examples

User: "Let's commit and push these changes" → `push` mode.
User: "Let's go ahead /git-pushing push" → `push` mode, explicit.
User: "Use /git-pushing to update the PR" / "/git-pushing pr" → `pr` mode.
User: "Go ahead commit push and merge" / "commit and push, merge to main through pr and return to main" / "2 and also merge the PR into main, switch locally to main, pull and delete old feature branch" → `full` mode.
User: "did you remove the feature branch?" → check the last `full`-mode run actually deleted it; if not, delete it now and confirm.
