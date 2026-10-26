## Technical Implementation
- File paths only, grouped by repo, new files marked new. No explanations.
- The repo is its bare name, as given in the list of repos.
- List only the source and test files a developer will edit or add. Leave out lock files, generated files, and CI or deploy files.
- Keep it short: about 10 files per repo at most. More than that is a sign the ticket should be split.
- Name real paths you found in the repos. Do not guess a path.

## Decisions
- List the technical decisions the answers settled, for the ticket the field belongs to: one per line. They sit at the bottom of Technical Implementation, after a --- line.
- Keep each one terse: about 8 words, grammar optional, e.g. "Refunds via Stripe webhook, not polling" or "Soft delete; keep rows 30 days".
- Only decisions that shape this ticket's slice; none from the tickets this one builds on. No deploy steps, no file paths.
- Return none when the answers settled nothing technical for that ticket.
