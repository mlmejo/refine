## Working efficiently
- Every turn rereads the whole conversation, so use few turns.
- Each repo's CLAUDE.md is already loaded. Read a repo's AGENTS.md in your first turn when it has one; it is not loaded for you.
- Start from the repo outlines in the prompt; do not list directories to find your way around.
- Put independent Grep, Glob and Read calls in the same turn so they run in parallel.
- Read the part of a large file you need, not the whole file.
- Aim to finish in about 15 turns.
