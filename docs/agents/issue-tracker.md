# Issue tracker: Beads

Issues and specs for this repo live in the Beads issue tracker and are managed with the `bd` CLI.

## Conventions

- **Find ready work**: `bd ready`
- **Create an issue**: `bd create --title="..." --description="..." --type=task|bug|feature --priority=2`
- **Read an issue**: `bd show <id>`
- **Claim an issue**: `bd update <id> --claim`
- **Update an issue**: `bd update <id> --title/--description/--notes/--design`
- **Close an issue**: `bd close <id> --reason="..."`
- **Manage dependencies**: `bd dep add <issue> <depends-on>`
- **Persist project knowledge**: `bd remember --key <key> "..."`; search with `bd memories <keyword>`
- **Validate issue conventions**: `bd lint` and `bd doctor`
- **Sync tracker data**: `bd dolt push`

Use non-interactive `bd` commands. Do not create parallel markdown issue files or use GitHub/GitLab issue commands for this repository.

## When a skill says "publish to the issue tracker"

Create a Beads issue with `bd create`, including a useful title, description, type, priority, and acceptance criteria when applicable.

## When a skill says "fetch the relevant ticket"

Run `bd show <id>`.

## Pull requests as a request surface

No. Pull requests are not a separate triage input surface for this workflow; track actionable work as Beads issues.
