# refine desktop

Electron app over the same Go code as the TUI. It runs `refine serve`, which
listens on 127.0.0.1 with a random port and token, and talks to it through the
main process, so the token never reaches the page. It shares `refine.db`,
`hidden.json` and `config.toml` with the TUI.

```sh
pnpm install          # once; if Electron's binary is missing: node node_modules/electron/install.js
pnpm dev              # builds ../bin/refine, then runs the app with hot reload
pnpm dist             # builds dist/mac*/refine.app with the binary bundled
```

The app runs refine from the project root, so refine reads the Jira token
from the project's `.env` as it does in the TUI (or from `REFINE_JIRA_TOKEN`).
A packaged app has no project root next to it: set `REFINE_DIR` to the repo. It takes
`claude` and `git` from your login shell's PATH. `REFINE_REF=develop` (or
`--ref=develop`) reads the repos at a branch, as `refine --ref` does.
`REFINE_BIN` points it at another refine binary.
