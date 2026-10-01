# CLAUDE.md

Operating contract for this repo. Read before doing anything here.

---

## What this project is

**Nazar** — a real-time payments fraud detection platform. Hackathon prototype with a
production-shaped architecture.

| File | Role |
|---|---|
| `problem_statement.txt` | The brief |
| `PLAYBOOK.md`, `ARCHITECTURE.md` | **Superseded.** Prior-session drafts. Kept for provenance only — do not build from them |
| `REVIEW.md` | Adversarial audit of those two: 54 findings, F-01…F-81 |
| `docs/00`…`docs/07` | **The build spec.** This is what you implement |

`docs/` supersedes the two root docs everywhere they disagree. If you find yourself citing
`ARCHITECTURE.md` for a design decision, you are reading the wrong file.

---

## Standing constraints (from the user, binding)

1. **Prefer the simpler fix.** If two solutions work, take the one with fewer moving parts. See
   §"Simplest thing that works" below — deviating from those defaults needs a stated reason.
2. **Don't rabbit-hole.** The architecture is the deliverable; the code is the proof. Build the
   thinnest thing that demonstrates each claim, then stop.
3. **Optimise for presentation, stay implementation-sound.** Every architectural claim needs a
   visible artifact (`docs/07`). Never trade correctness for a demo beat — if a beat requires a lie,
   change the beat.
4. **Hosted LLM APIs are fine.** No local/offline models. All data is synthetic, so there is no
   privacy surface — say exactly that when asked (`docs/03 §13.1`).
5. **Keep it adaptable.** Everything behind a seam; mid-build reversals must be cheap
   (`docs/00 §3`).

---

## Non-negotiables

These exist because the review found each one violated. Breaking one is a bug, not a style choice.

| # | Rule | Why |
|---|---|---|
| 1 | **`ZCOUNT`, never `ZCARD`** for windows. `ZCARD` is lint-banned | F-32: the flagship feature was computed over the wrong window |
| 2 | **Read strictly before write.** A transaction is never in its own features | F-41 |
| 3 | **Every feature has a backing key.** `test_feature_catalogue_key_coverage` enforces it | F-34: six features had no storage |
| 4 | **Bind pipeline replies by name, never by position** | F-37: silent feature corruption |
| 5 | **No `BLOCK` from any degraded path, ever.** Cap value instead of denying | F-42, D7 |
| 6 | **Advisories cap below `HOLD`** | F-20: "worst case three seconds" was false |
| 7 | **Rings and novelty never block.** Blocking needs an analyst + four-eyes | F-26, F-27 |
| 8 | **Calibrate on a natural-prevalence, time-forward slice.** Assert it | F-08 |
| 9 | **Prevalence correction is explicit and versioned** | F-07: every rupee threshold depends on it |
| 10 | **Training query needs `available_at > decided_at`** | F-57 |
| 11 | **Server-derived values beat client claims.** `ClaimedFacts` is quarantined | F-43 |
| 12 | **Never say "created 3 days ago."** Say "first seen by us" | D8 |
| 13 | **Generator numbers are `[RECOVERED]`, never detection rates** | F-06 |
| 14 | **Raw `remittance_info` never reaches an LLM** | Prompt injection |
| 15 | **No `eval` for rules.** CEL only | F-72 |
| 16 | **Three latency numbers**: queue / service / total. Quote total | F-01 |

---

## Simplest thing that works

Where the docs describe a `[P1]` mechanism, **build the `[P0]` column** unless there's a reason not
to. The `[P1]` design is documented so the upgrade is a swap, not a rewrite — that's the point of the
seams.

| Concern | Build this at P0 | Not this (yet) |
|---|---|---|
| Audit chain | **One chain, one writer** (single decision instance) | Per-shard chains + Merkle anchor |
| Redis reads | **Plain pipelines per hash-tag group** | Redis Functions / Lua |
| Overload | **Fixed concurrency cap + shed to rails-only** | Adaptive limiter, LIFO, hedged reads |
| Calibration | **Beta calibration** (3 params, smooth, extrapolates) | Bootstrapped isotonic |
| Novelty | **Leaf-space kNN + conformal p-value** — reuses the scoring forward pass | Anything with a separate model |
| Consortium tokens | **HMAC + epoch field + honest framing** | OPRF (documented; ship if you claim privacy) |
| Off-policy eval | **Log `action_propensity`.** That's it | IPW / doubly-robust estimators |
| Decision storage | **Postgres, JSONB features, daily partitions** | Parquet archival tier |
| Graph | **In-process Go adjacency + decay + component cap** | Sharded RocksDB |
| Event log | **Redis Streams** | Redpanda |
| Deployment | **One binary, four goroutine pools** | Five services |

The interfaces are the same in both columns. That is the whole trick.

---

## Stack

Go (decision, profile-apply, graph, casework, fanout) · Python (training, eval, generator, drift) ·
Redis 7 cluster · Postgres 16 · CEL for rules · LightGBM → Treelite `.so` · React/TS console · SSE
fan-out · hosted Claude API for narrative + investigation agent (off the request path).

Rationale and flip conditions: `docs/00 §6`.

---

## Conventions

- **Money is `int64` minor units (paise). Never a float. Anywhere.**
- Windows key on `accepted_at_ms` (stamped by us), never the producer's `event_ts_ms`.
- `internal/features` may not import `internal/profile` or any client library — features are pure
  functions of `(ProfileBundle, Event)`. Lint-enforced.
- `proto/` changes require an ADR. Nothing else does.
- Every signal has three states: `off | shadow | live`. New signals ship in `shadow`.
- Every decision stamps `model_bundle_version`, `policy_version`, `rules_version`,
  `signal_registry_version`.
- Feature IDs are immutable. A semantic change is a new ID, not an edit.

---

## Before you say a number out loud

Tag it. `[MEASURED]` (real labels) · `[RECOVERED]` (our generator) · `[MODELLED]` (arithmetic on a
stated assumption). And `[P0]` (measured on the prototype) vs `[P1]` (modelled).

The claims register and the delete-these-sentences table are in `docs/06 §5`. Read it before
rehearsal.

**Unverified as of 2026-08-14** — re-check against primary sources before the pitch: DPIP status
(rbihub.in, not news aggregators), the RBI Authentication Directions text (the circular itself, not
law-firm summaries), UPI volumes (NPCI). See `docs/05 §4.7`.

---

## Where things are

| Need | File |
|---|---|
| Why a prior decision was wrong | `REVIEW.md`, search the F-number |
| Deployment profiles, seams, stack, trust boundaries | `docs/00` |
| Latency measurement, deadlines, overload, HA, capacity | `docs/01` |
| Event schema, Redis keys, features, guards, Postgres DDL | `docs/02` |
| Training data, calibration, rules, novelty, feedback, adversarial, LLM lane | `docs/03` |
| Decision rule, rails, fast path, advisory, degradation | `docs/04` |
| Ring weights, graph maintenance, consortium, tokens | `docs/05` |
| Build order, what to stub, invariant tests, claims register | `docs/06` |
| Screens, visual system, demo script, Q&A | `docs/07` |

---

## The one paragraph that matters

The previous architecture was rhetorically excellent and unbuildable: a feature computed over the
wrong window, storage that couldn't produce three of its own features, DDL that doesn't create, a
hard block on "three people paid the same merchant," and a self-audit that congratulated itself for
catching the class of bug it then committed. **The rebuild's advantage is not that it's more
elaborate — it's that every claim has a test and every number has a tier.** Keep it that way, and
when in doubt, take the simpler option and write down why.
