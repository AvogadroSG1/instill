---
name: git-pushing
description: Run Peter's git workflows — commit+push, full branch → PR → merge → return-to-main → pull → delete-branch cycle, or tag and create a release. Use whenever Peter asks to push, commit, merge, PR, tag, release, or finish up a branch, including phrases like "commit and push", "push this", "let's push these commits", "merge into main", "PR and merge", "tag and release", "cut a release", "release v1.0.0", "go ahead commit push and merge", "did you push?", "did you remove the feature branch?", or invocations like "/git-pushing", "/git-pushing push", "/git-pushing pr", "/git-pushing full", "/git-pushing release", "/git-pushing --tag v1.0.0". Always use this instead of running raw git commands one-by-one when the request matches these patterns.
---

# Git Push Workflow

Peter has asked for this same handful of git sequences hundreds of times over the past several months, worded differently each time ("commit and push", "PR and merge into main and return to main", "did you remove the feature branch?", "go ahead commit, push to feature branch, merge through pr, return to main, and pull", "tag and release v1.0.0"). The point of this skill is to stop making him spell out the steps — recognize which mode applies from the wording and run it.

## The Modes

Figure out which mode applies from the wording. Default to **push** when in doubt — it's the smallest, safest action and matches how Peter phrases most requests ("commit and push", "let's push these commits"). Only escalate to `pr`, `full`, or `release` when he actually asks for a PR, merge, main, tag, or release.

| Mode / Option | Trigger phrases | What it does |
|---------------|------------------|--------------|
| `push` (default) | "commit and push", "push this", "save this to github", "/git-pushing push", "/git-pushing" with no other detail | Branch (if needed) → stage → commit → push. Stops there. (If `--tag` specified, creates tag, pushes tag, and publishes release). |
| `pr` | "PR this", "open a PR", "/git-pushing pr", "commit and push and PR" | Everything in `push`, then opens the PR. Does not merge. |
| `full` | "merge into main", "PR and merge", "go ahead commit push and merge", "return to main", "commit, push, PR, merge, return to main and pull", "delete the feature branch" | Everything in `pr`, then merges, switches back to main, pulls, tags/releases (if `--tag` passed), and deletes the feature branch. |
| `release` / `--tag` | "tag as v1.0.0", "tag and release v1.0.0", "create a release v1.2.0", "cut a release v2.0.0", "/git-pushing --tag v1.0.0", "/git-pushing release --tag v1.0.0" | Creates annotated git tag, pushes tag to origin, and publishes GitHub Release via `gh release create`. |

Peter's actual phrasing is rarely this clean — he mixes and matches ("commit and push, merge to main through pr, return to main, and release v1.0.0"). Read the whole request for the *furthest* step he mentions (tag/release/merge/main/delete-branch beats PR beats push) and run everything up to and including it.

### Release & Tag Sequencing Rule

- **In `full` mode with a tag:** The tag and GitHub release **MUST** be created on the default branch (`main`) **after** the PR is merged and pulled (`git checkout main && git pull`). Tagging before merge tags the feature branch instead of the merged commit on main.
- **In `push` / `release` mode with a tag:** The tag is created on the current HEAD after pushing the commit.

## Why branching is non-negotiable

Peter has explicitly corrected this before ("Always make a feature branch" / "why don't you just make a feature branch"). **If the current branch is `main` or `master` and changes exist, always create a feature branch before committing** — never commit straight to main, even in `push` mode. Name it descriptively based on the change (`feat/...`, `fix/...`, `docs/...`, `release/...`), matching the conventional-commit type you're about to use. If already on a non-main feature branch, just use it. (If the repo is clean and you are purely tagging/releasing existing commits on main, stay on main).

## Running it

```bash
bash scripts/smart_commit.sh                                      # push mode, auto-generated message
bash scripts/smart_commit.sh "feat: message"                       # push mode, explicit message
bash scripts/smart_commit.sh --pr                                  # pr mode
bash scripts/smart_commit.sh --pr "feat: message"
bash scripts/smart_commit.sh --full                                # full mode: PR + merge + return to main + pull + delete branch
bash scripts/smart_commit.sh --full "feat: message"
bash scripts/smart_commit.sh --tag v1.0.0                          # push + tag + GitHub release
bash scripts/smart_commit.sh --full --tag v1.0.0                   # full merge + tag & release on main + delete branch
bash scripts/smart_commit.sh --full --tag v1.0.0 "feat: message"   # full with explicit commit message and tag
bash scripts/smart_commit.sh --release v1.0.0                      # release mode: tag & publish release
bash scripts/smart_commit.sh -t v1.0.0 --title "v1.0.0" --notes "Release notes"
bash scripts/smart_commit.sh -t v1.0.0-rc1 --prerelease           # pre-release flag
bash scripts/smart_commit.sh -t v1.0.0 --draft                    # draft release flag
```

The script handles:
- Creating a feature branch if you're on main/master and have uncommitted changes
- Staging, generating a conventional commit message from the diff (or using the one you pass in)
- Committing with **both** required co-authors (see below — this is not optional, it's a hard requirement from Peter's CLAUDE.md / AGENTS.md)
- Pushing (`-u` for new branches)
- `--pr`: opening the PR via `gh pr create` and printing the URL
- `--full`: merging the PR, switching back to main, pulling, applying tag & release (if requested), and deleting the local + remote feature branch
- `--tag <tag>`: creating annotated git tag (`git tag -a`), pushing tag (`git push origin <tag>`), and creating GitHub Release via `gh release create <tag> --generate-notes` (with optional `--title`, `--notes`, `--notes-file`, `--draft`, `--prerelease`)

If `gh` isn't available or the repo has no GitHub remote, fall back to the manual path below for the PR/merge/release steps and tell Peter why.

## Manual path (script unavailable or custom situation)

**1. Check status and branch** — `git status`, `git branch --show-current`. If on main/master and changes exist, create and switch to a feature branch first.

**2. Stage** — `git add .` (or specific files for a partial commit).

**3. Commit message** — conventional format `type(scope): description`, imperative mood, types `feat|fix|refactor|docs|test|chore`. If Peter gave a message, use it verbatim.

**4. Co-authors — required, not optional.** Peter's global instructions require every commit to carry both co-authors. Don't use the generic tool footer alone — this has been a recurring correction point:

```bash
git commit -m "$(cat <<'EOF'
type(scope): description

Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
```

(Swap the tool name/model if you're running under another harness — e.g. `opencode` / `Codex`.)

**5. Push** — `git push`, or `git push -u origin <branch>` for a new branch.

**6. If `pr` or `full`:** open the PR (`gh pr create --title ... --body ...`), listing Peter and the CLI tool as co-authors in the body. Share the PR URL.

**7. If `full`:** once the PR is mergeable, merge it (`gh pr merge --merge`), `git checkout main`, `git pull`.

**8. If `tag` / `release`:** create the annotated tag, push tag, and publish the GitHub release:
```bash
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
gh release create v1.0.0 --generate-notes
```

**9. If `full`:** delete the feature branch both locally (`git branch -d <branch>`) and on remote (`git push origin --delete <branch>`) — this last step is easy to forget and Peter has had to ask "did you remove the feature branch?" more than once. Don't skip it.

**10. Confirm** — report the commit hash, PR link (if any), tag/release URL (if any), and final branch/sync state so Peter doesn't have to ask "did you push?" / "is this merged?" / "is the release published?" afterward.

## Examples

- User: "Let's commit and push these changes" → `push` mode.
- User: "Let's go ahead /git-pushing push" → `push` mode, explicit.
- User: "Use /git-pushing to update the PR" / "/git-pushing pr" → `pr` mode.
- User: "Go ahead commit push and merge" / "commit and push, merge to main through pr and return to main" / "2 and also merge the PR into main, switch locally to main, pull and delete old feature branch" → `full` mode.
- User: "PR, merge to main, and tag as v1.0.0" → `full` mode with `--tag v1.0.0` (`bash scripts/smart_commit.sh --full --tag v1.0.0`).
- User: "Commit, push, and release v1.2.0" → `push` mode with `--tag v1.2.0` (`bash scripts/smart_commit.sh --tag v1.2.0`).
- User: "Tag this repo as v0.5.0 and create a GitHub release" → `release` mode (`bash scripts/smart_commit.sh --release v0.5.0`).
- User: "did you remove the feature branch?" → check if the last `full`-mode run deleted it; if not, delete it now and confirm.
