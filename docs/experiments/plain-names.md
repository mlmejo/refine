# Experiment: plain names in findings and questions

Written 2026-10-21, before the first ticket was assigned. The metrics and the
analysis below are fixed; they are not changed after seeing results.

## Log

- 2026-10-21: started, prompts `d5ea28ed47`.
- 2026-10-21: **restarted**, prompts `50bf4ca03e`. Every question now quotes the
  ticket text it serves (`anchor`), questions the ticket does not ask for
  are left out, the app shows the full ticket, and "I don't know yet" is an
  answer. That changes the prompt in both arms, so the one ticket assigned
  before (GEEK-12179) was dropped from the experiment and pairing starts
  over. Only tickets assigned from here count toward the 20.
- 2026-10-21: prompts `f239e50cf1`, before any ticket was assigned after the
  restart, so no restart was needed. Every run now pins the model
  (`claude-opus-5-5`) and loads project and local settings only, so the
  developer's own hooks and plugins (a terse-style hook among them) no
  longer reach Claude. Their global CLAUDE.md still loads: only `--bare`
  drops it, and `--bare` cannot use a Claude plan login.

## Question

Does writing findings, questions, options and consequences in plain words,
with the code behind each plain name on hover, lead to better refinement
decisions than prose with inline code?

## Arms

- **A, inline code.** The questions step as it is: identifiers and file
  references written into the prose (shown as chips).
- **B, plain names.** The questions step adds `plain.md`: things in the code
  are named in plain words, each plain name is listed once in `terms` with
  its identifiers and locations, and the prose holds no identifiers or paths.
  The app underlines each plain name and shows its code on hover.

Everything else is the same in both arms: the decision deck, Claude's pick
never shown, a neutral consequence per option, every question anchored in
the ticket's text, "I don't know yet" as an answer, disagreement only as a
reopened question citing the code, and stamping only once a round comes
back with no questions.

## Assignment

- The unit is a ticket. Its arm is drawn on its first questions round and
  kept for every later round and the stamp (`experiments` table, `Draft.arm`).
- Tickets go in balanced pairs: the first of each pair is a coin flip, the
  second gets the other arm.
- Tickets refined before the experiment started stay out of it.
- Turned on by `[experiments] plain_names = true` in `config.toml`.

## Horizon

20 stamped tickets, 10 per arm. The analysis runs once, at the end; results
are not looked at before then.

## Metrics

**Primary: decisions that did not hold.** Per ticket, the number of earlier
decisions that a later round reopened (`run` events with `kind=questions`,
summed `reopened`) divided by the decisions made by the stamp (`run` event
with `kind=stamp`, `decisions`). Compared as the mean per arm, with the
spread across tickets.

**Secondary:**

- time per decision (`decide.ms`, first answer per question)
- rounds until the questions were clear (`run` events with `kind=questions`)
- answers changed after the first pick (`decide` with `first=false`)
- confidence at stamp, 1 to 5 (`confidence.value`)
- agreement with Claude's hidden pick (`answer.agreed`), to see whether
  either arm moves it

**Arm B check:** share of terms hovered (`term_hover`, against `terms` in
the questions run). Near every term means the plain names are too vague.

**Later, slower:** whether the written AC or Technical Implementation was
edited in Jira during the sprint, and QA bounces, from the Jira changelog.

## Known limits

- One developer, and they know which arm a ticket is in; the behavioural
  metrics hold up better against that than confidence does.
- Tickets differ in difficulty; random pairs spread that, not remove it.
- With 10 tickets per arm, only a large difference (roughly 15 to 20 points
  on the primary metric) can be told apart from noise.
- Arm B changes Claude's output as well as its display: the test is of the
  whole treatment.
- refine keeps changing during the test. Every event records the build
  (commit, `-dirty` for uncommitted changes) and `prompts`, a fingerprint of
  every rule and schema Claude is given. A change to the prompts mid-way is a
  confound: results are also reported per `prompts` version, and any prompt
  change during the test is noted here with its date.

## Analysis

`plain-names.sql` computes every metric per arm from `refine.db`:

    sqlite3 ~/.local/state/refine/refine.db < docs/experiments/plain-names.sql
