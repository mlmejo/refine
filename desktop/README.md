# refine desktop

The Electron app refine runs as. It starts `refine serve`, which listens on
127.0.0.1 with a random port and token, and talks to it through the main
process, so the token never reaches the page. refine keeps its state in
`~/.local/state/refine` (`refine.db`, `hidden.json`, the worktrees Claude
reads) and its repos in `~/.config/refine/config.toml`.

```sh
pnpm install          # once; if Electron's binary is missing: node node_modules/electron/install.js
pnpm dev              # builds ../bin/refine, then runs the app with hot reload
pnpm dist             # builds dist/mac*/refine.app with the binary bundled
```

refine reads the Jira token from `~/.config/refine/config.toml`, or from
`REFINE_JIRA_TOKEN`. The app takes `claude` and `git` from your login shell's
PATH. `REFINE_BIN` points it at another refine
binary, and `XDG_STATE_HOME` at another state folder, e.g. to try it without
touching your refinements.
