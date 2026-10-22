-- Metrics for docs/experiments/plain-names.md, per arm.
.headers on
.mode column

-- Tickets in the experiment that were stamped.
WITH arms AS (
  SELECT key, arm FROM experiments WHERE name = 'plain_names'
),
stamped AS (
  SELECT key, max(json_extract(props, '$.decisions')) AS decisions
  FROM events WHERE name = 'run' AND json_extract(props, '$.kind') = 'stamp' AND json_extract(props, '$.ok') = 1
  GROUP BY key
),
rounds AS (
  SELECT key, count(*) AS rounds, sum(json_extract(props, '$.reopened')) AS reopened
  FROM events WHERE name = 'run' AND json_extract(props, '$.kind') = 'questions' AND json_extract(props, '$.ok') = 1
  GROUP BY key
),
firsts AS (
  SELECT key, avg(json_extract(props, '$.ms')) / 1000.0 AS secs_per_decision,
         sum(json_extract(props, '$.first') = 0) AS changed_after_first
  FROM events WHERE name = 'decide' GROUP BY key
),
confidence AS (
  SELECT key, avg(json_extract(props, '$.value')) AS confidence FROM events WHERE name = 'confidence' GROUP BY key
),
agreement AS (
  SELECT key, avg(json_extract(props, '$.agreed')) AS agreement
  FROM events WHERE name = 'answer' AND json_extract(props, '$.agreed') IS NOT NULL GROUP BY key
),
per_ticket AS (
  SELECT a.key, a.arm, s.decisions,
         coalesce(r.reopened, 0) * 1.0 / nullif(s.decisions, 0) AS did_not_hold,
         r.rounds, f.secs_per_decision, f.changed_after_first, c.confidence, g.agreement
  FROM arms a JOIN stamped s USING (key)
  LEFT JOIN rounds r USING (key) LEFT JOIN firsts f USING (key)
  LEFT JOIN confidence c USING (key) LEFT JOIN agreement g USING (key)
)
SELECT arm, count(*) AS tickets,
       round(avg(did_not_hold), 3) AS did_not_hold_mean,
       round(min(did_not_hold), 3) AS did_not_hold_min,
       round(max(did_not_hold), 3) AS did_not_hold_max,
       round(avg(rounds), 2) AS rounds,
       round(avg(secs_per_decision), 1) AS secs_per_decision,
       round(avg(changed_after_first), 2) AS changed_after_first,
       round(avg(confidence), 2) AS confidence,
       round(avg(agreement), 2) AS agreement_with_hidden_pick
FROM per_ticket GROUP BY arm;
