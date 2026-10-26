## Acceptance Criteria
- Each bullet is one plain sentence describing something QA can see in the product. No headline, no label, no numbering, no bold, no Given/When/Then.
- No class names, endpoints, event names, queues, ticket codes or other implementation words; those belong in Technical Implementation.
- Keep each bullet short: about 12 words, 15 at most, one behaviour each. Break a bullet that joins two with "and", "even when" or a semicolon into two. No parenthetical examples.
- Keep them general. Do not write conditions QA cannot check from the product alone, such as "recorded once" or "only when X actually changed"; those belong in Technical Implementation.
- Cover edge cases, and mark expected behaviour that could look like a bug with "This is expected and not a defect." as a second sentence; it does not count toward the word limit.
- Aim for 4 to 8 bullets.
- A ticket with no user-visible behaviour, such as a package install, a migration or wiring, gets one bullet instead: "No user-facing behaviour; verified by tests and code review." This applies to split tickets too.

Example of the expected style:
* Clicking Delete Account is recorded.
* Completing a deletion is recorded, along with how long the account had existed and the credits left on it.
* Starting a deletion without completing it is reported as a cancellation.
* A figure with no events in the range reads "not yet instrumented" rather than zero.

## Technical Implementation
- File paths only, grouped by repo, new files marked new. No explanations.
- The repo is its bare name, as given in the list of repos.
- List only the source and test files a developer will edit or add. Leave out lock files, generated files, and CI or deploy files.
- Keep it short: about 10 files per repo at most. More than that is a sign the ticket should be split.
- Name real paths you found in the repos. Do not guess a path.
