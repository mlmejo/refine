# refine

refine refines Jira tickets with Claude Code. Pick a ticket from the
refinement sprint and it walks you through one step after another:

1. **Related**: the tickets that overlap this one, and which of them block it.
2. **Repos**: the repos the work touches. refine fetches each one's `develop`
   (or origin's default branch) into its own worktree, so your checkout is
   never touched.
3. **Questions**: up to 3 rounds of up to 5 questions, asked after Claude has
   read the code.
4. **Deploy**: what shipping the change needs, per repo.
5. **Results**: Acceptance Criteria, Technical Implementation, points and any
   split tickets, for you to review.
6. **Stamp**: writes the fields, comments the deploy needs, creates the split
   tickets, links the blockers and moves the ticket to the ready sprint.

Go decides everything with one right answer: which tickets to show, how many
questions, the points, the Jira writes. Claude (`claude-opus-5-5`, run
headless and read-only) only judges.

## Requirements

- macOS
- Go 1.26
- Node and pnpm
- [Claude Code](https://claude.com/claude-code) (`claude`) and `git` on your
  login shell's PATH
- An Atlassian API token

## Setup

Create `~/.config/refine/config.toml`:

```toml
[jira]
site = "your-site.atlassian.net"
email = "you@example.com"
# Optional; these are the defaults.
project = "GEEK"
sprint = "Refinement Ready - Team John"
ready_sprint = "Ready for Sprint - Team John"

[repos]
api = "~/develop/api"
web = "~/develop/web"
```

Repos can also be added and removed from the app. Put the token in `.env` at
the repo root, or export it:

```sh
REFINE_JIRA_TOKEN=...
```

## Run

```sh
cd desktop
pnpm install
pnpm dev     # builds bin/refine, then runs the app with hot reload
pnpm dist    # builds dist/mac*/refine.app with the binary bundled
```

See [desktop/README.md](desktop/README.md) for how the app talks to the
backend and the environment variables it reads (`REFINE_DIR`, `REFINE_BIN`,
`XDG_STATE_HOME`).

refine keeps its state in `~/.local/state/refine`: `refine.db`, `hidden.json`
and the worktrees Claude reads.

## Layout

```
cmd/refine          the backend binary: refine serve, refine version
internal/refine     the step flow, prompts and output schemas, the stamp
internal/claude     runs headless Claude Code for one step
internal/jira       the Jira client
internal/server     the HTTP API the app calls
internal/store      refinements and deploy profiles in SQLite
internal/gitsync    fetches the branch Claude reads
internal/worktree   checks it out into a refine-owned worktree
desktop             the Electron app
spike               the original headless-Claude spike
```

## Test

```sh
go test ./...
cd desktop && pnpm typecheck
```

## Flags

`VITE_RERUN_QUESTIONS=1` at build time shows a button that drops the question
rounds and asks round 1 again.
