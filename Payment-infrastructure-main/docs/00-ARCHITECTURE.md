# 00 — System Architecture

**Status:** ready to build · **Supersedes:** `ARCHITECTURE.md`, `PLAYBOOK.md §05`
**Companion review:** [REVIEW.md](../REVIEW.md)

---

## Contents

- [1 — Deployment profiles: the rule that prevents every overclaim](#1--deployment-profiles)
- [2 — Design principles](#2--design-principles)
- [3 — Evolvability: how to change your mind mid-build](#3--evolvability)
- [4 — Topology](#4--topology)
- [5 — Service decomposition](#5--service-decomposition)
- [6 — Technology choices](#6--technology-choices)
- [7 — Trust boundaries](#7--trust-boundaries)
- [8 — Failure domains](#8--failure-domains)
- [9 — Repository layout](#9--repository-layout)
- [10 — Configuration and policy](#10--configuration-and-policy)

---

## 1 — Deployment profiles

**Every number in every document carries a profile tag. No exceptions.** This single rule
eliminates the largest class of finding in the review (F-69, F-78, F-81): the previous documents
made prototype engineering choices and then defended them with production-scale arguments.

| Tag | Profile | Load | Purpose |
|---|---|---|---|
| **`[P0]`** | Prototype / demo | ≤ 200 TPS, ~2k accounts, single node | What you build and demo. Every number you say out loud comes from here |
| **`[P1]`** | Bank-scale reference | 3–8k TPS sustained, 25k peak, 20M accounts | What the architecture is *designed to admit*. Numbers are modelled, and labelled as modelled |
| **`[P2]`** | National / DPIP-scale | 25–40k TPS peak, 300M+ accounts | Named, costed at the component level, **not built**. See [06-BUILD-PLAN §6](06-BUILD-PLAN.md#6--out-of-scope) |

Rules:

1. A `[P0]` measurement may never be presented as a `[P1]` capability.
2. A `[P1]` model may never be presented as a measurement.
3. When asked "does this scale", the answer names the profile, the bottleneck, and the change
   required — never "scoring is stateless."

**The honest scale answer, memorised:**

> "At P0 we measure. At P1 we've modelled it, and the binding constraint is the profile store's
> memory and the pair keyspace — here's the sizing. At national scale the graph and the pair
> keyspace both need replacing, and I can tell you with what."

---

## 2 — Design principles

Nine. Each one exists to prevent a specific named failure. Carried forward from the previous
architecture where it was right (P1–P8 there → D1–D6 here), with three additions.

**D1 — Read, never recompute.** Nothing downstream of the profile load touches raw history. This
makes the tail defensible *and* makes replay a read rather than a reconstruction.
*(Kept from previous P1/P2.)*

**D2 — Materialise features at decision time; persist them with the decision, with their
provenance and staleness.** Extends the previous P2: it is not enough to store the vector, you must
store *how fresh each part of it was*, or replay is a lie under load (fixes F-30).

**D3 — Every threshold is relative to the entity's own baseline**, except regulatory rails, which
are absolute by law, and are labelled as such with an effective-date. *(Kept from previous P3.)*

**D4 — A signal that cannot explain itself cannot cross a boundary.** Mandatory non-empty
explanation, enforced at construction. *(Kept verbatim from previous P6 — the best idea in the
original document.)*

**D5 — Not-applicable and not-evaluated are distinct from clean.** Four-state signal status. A
check that never ran must never render as a check that passed. *(Kept verbatim from previous P8.)*

**D6 — No feature and no decision input derives from our own prior decisions** — and the data-side
complement: **the training set must contain traffic we did not action.** The previous P7 closed the
feature path and left the data path open (F-12). Both are closed here: see
[03-ML-PIPELINE §7](03-ML-PIPELINE.md#7--the-feedback-loop-and-how-we-break-it).

**D7 — Degrade to *more friction*, never to a block that would not have occurred healthy, and
never silently to allow.** The previous document stated this and then violated it in the degraded
velocity cap (F-42). Here it is a property test that runs in CI over every injected failure, and
the degraded path caps *value*, never denies.

**D8 — Every claim in the product must be computable from what we actually observe.** If the
system cannot know a beneficiary's account-opening date, the UI may not say "this account was
created 3 days ago" (F-43). It says what we know: "we first saw this account 3 days ago." This is a
UI-copy rule *and* a feature-naming rule: `payee_first_seen_by_us_days`, not `payee_age_days`.

**D9 — Every component behind a seam.** See §3. This is the principle that makes the other eight
survivable when one of them turns out to be wrong mid-build.

---

## 3 — Evolvability

> *"If we find flaws mid-build it must be adaptable rather than rigid."*

This is a design requirement, and it is designed for explicitly rather than hoped for. Three
mechanisms.

### 3.1 — Classify every decision by reversal cost, and know which is which

| Class | Meaning | Rule |
|---|---|---|
| **Type 1 — one-way** | Reversal costs > 1 week | Requires an ADR, a spike, and a written fallback before commitment |
| **Type 2 — two-way** | Reversal costs < 1 day | Decide fast, in code, move on. Do not hold a meeting |

**The Type 1 list for this system is short, and it is the only list you have to be careful about:**

| Decision | Why it's one-way | Escape hatch built in from day one |
|---|---|---|
| Canonical event schema | Every feature, rule, model and stored vector binds to it | Versioned envelope + additive-only evolution (§3.3) |
| Feature *identity* (name → semantics) | Persisted vectors and trained models bind to names | Feature registry with immutable IDs; renames create a new ID |
| Decision record schema | It is the audit chain; you cannot rewrite history | Additive columns + a `schema_version` on every row |
| Consortium wire format | Other institutions parse it | `v` field, and readers must ignore unknown fields |
| Money units (paise, integer) | Silent corruption if changed later | Never changes |

**Everything else is Type 2 and is explicitly designed to be swappable:** the model, the calibrator,
the novelty method, the rule engine, the graph store, the profile store backend, the transport, the
language of any individual service. If you find a flaw in one of them in week two, you replace it
behind its interface and the rest of the system does not know.

### 3.2 — Ports and adapters, with the seams named up front

Every replaceable component sits behind a narrow interface. These are the seams. Cutting them
correctly on day one is what buys adaptability later; adding them retroactively is what does not
happen.

```go
// The five seams. Everything behind these is replaceable without touching callers.

type ProfileStore interface {
    Load(ctx context.Context, ev *Event) (*ProfileBundle, error)  // Redis → Dragonfly → Aerospike
    Apply(ctx context.Context, ev *Event) error
}

type Signal interface {                     // rule | model | novelty | graph | consortium
    Meta() SignalMeta                       // name, kind, advisory, rails, requires, version
    Evaluate(ctx context.Context, sc *ScoringContext) ([]Finding, error)
}

type Scorer interface {                     // LightGBM → XGBoost → NN → a different model per rail
    Score(fv FeatureVector) (raw float64, contribs map[string]float64, err error)
    Meta() ModelMeta
}

type Calibrator interface {                 // beta → isotonic → temperature scaling
    Calibrate(raw float64) float64
    Meta() CalibratorMeta
}

type DecisionSink interface {               // sync PG → WAL+async → Kafka → whatever
    Emit(ctx context.Context, d *Decision) error
}
```

Rules that keep the seams real:

- **No caller may type-assert through a seam.** Enforced by a lint rule.
- **Every seam has ≥ 2 implementations from day one** — the real one and a deterministic fake used
  in tests. A seam with one implementation is not a seam; it is a comment.
- **The `Signal` list is data, not code.** Adding, removing, or reordering a signal is a config
  change, not a deploy. This is what makes "we were wrong about novelty" a five-minute fix.

### 3.3 — Change without redeploy: the four dials

| Dial | Changes at | Governs | Rollback |
|---|---|---|---|
| **Signal registry** | Runtime (config push) | Which signals run, and whether each is advisory, shadow, or off | Instant, per signal |
| **Policy bundle** | Runtime (versioned, four-eyes) | Thresholds, rails, segment operating points, ladder caps | Instant, to any prior version |
| **Model bundle** | Runtime (registry pin) | Model + calibrator + feature spec, as one signed artefact | Instant, to any prior version |
| **Rule bundle** | Runtime (versioned, four-eyes) | Rails and rule-features | Instant, to any prior version |

Every one of these is **stamped by version onto every decision record**, so any decision can be
explained with the exact configuration that produced it, and any change can be A/B'd and reverted.

**Every signal has three states, not two:** `off | shadow | live`. `shadow` means it computes,
emits findings, and is persisted — but contributes nothing to the decision. **This is the mechanism
for adaptation:** when you suspect a component is wrong, you drop it to `shadow` and keep
collecting evidence with zero customer impact. It is also how you fix novelty (F-22): it ships in
`shadow` and only goes `live` when its measured precision justifies it.

### 3.4 — Decision log, not decision archaeology

Keep `docs/adr/NNN-*.md`, one page each: context, options, decision, consequences, **and a
"revisit when" trigger.** The trigger is the part that matters:

> *ADR-004: Novelty via leaf-space conformal p-value. **Revisit when:** precision at the operating
> threshold measured on ≥ 500 matured labels falls below 0.15, or when the false-positive burden on
> the unknown-pattern queue exceeds one analyst-hour per day.*

A decision with a written revisit trigger is a decision you can reverse without it feeling like a
failure. That is the whole mechanism.

### 3.5 — Build in vertical slices, not horizontal layers

The previous build order is layer-by-layer (generator → profile store → scoring → rules → …), which
means you do not find out whether the system works until near the end. Build **one typology
end-to-end first** — generator → ingest → features → rules → decision → persisted → on screen —
then add typologies. Every flaw surfaces in week one, when changing your mind is cheap.
See [06-BUILD-PLAN §2](06-BUILD-PLAN.md#2--build-order).

---

## 4 — Topology

**The single most important architectural correction:** the previous documents described two
incompatible topologies (F-02). This one commits.

**Nazar is a synchronous inline advice service.** The bank's payment switch calls it with a
deadline and has a default action on timeout. It is not the owner of the payment. It is not
stream-driven on the hot path.

```
                        ┌─────────────────────────────────────────┐
   payment switch ──────▶  POST /v1/decide       deadline: 25 ms  │
   (or demo PWA)   ◀─────  Decision                               │
                        └───────────────┬─────────────────────────┘
                                        │
        ┌───────────────────────────────┼──────────────────────────────┐
        │  DECISION SERVICE (Go)  ── stateless, N replicas             │
        │                                                              │
        │  1. accept + stamp accepted_at        ~10 µs                 │
        │  2. local rails check (in-proc filters, zero I/O)  ~2 µs     │
        │  3. profile load — K concurrent single-slot pipelines        │
        │     ├── payer slot ──┐                                       │
        │     ├── payee slot ──┤  wall clock = max(RTT), not sum       │
        │     ├── device slot ─┤                        ~0.3 ms p50    │
        │     └── pair slot ───┘                                       │
        │  4. feature assembly (pure, no I/O)   ~20 µs                 │
        │  5. signals: rules · model · graph(cached) · consortium      │
        │     · novelty(shadow)                 ~80 µs                 │
        │  6. calibrate → decide → attribute (SHAP)  ~300 µs           │
        │  7. WAL append (local, fsync-batched)      ~50 µs            │
        │  8. RESPOND ◀────────────────────────────────────────        │
        │  9. hand off to async lane (bounded, separate goroutine pool)│
        └────────┬──────────────────────────────┬──────────────────────┘
                 │                              │
        ┌────────▼─────────┐          ┌─────────▼──────────┐
        │  PROFILE STORE   │          │  EVENT LOG         │
        │  Redis Cluster   │          │  Redis Streams[P0] │
        │  hash-tagged     │          │  Redpanda    [P1]  │
        └──────────────────┘          └─────────┬──────────┘
                                                │
        ┌───────────────────┬───────────────────┼──────────────────┐
        ▼                   ▼                   ▼                  ▼
   ┌─────────┐        ┌──────────┐        ┌──────────┐      ┌──────────┐
   │ PERSIST │        │  GRAPH   │        │  CASES   │      │ FAN-OUT  │
   │ Postgres│        │ service  │        │ service  │      │ SSE → UI │
   │ per-shrd│        │ incr.    │        │ grouping │      │ coalesced│
   │ chain   │        │ compnts  │        │ narrative│      │ + sampled│
   └─────────┘        └──────────┘        └──────────┘      └──────────┘
```

**Why the event log is off the hot path but still present.** The decision service responds from its
own local WAL. The log is the durable spine for everything asynchronous: persistence, graph
maintenance, case grouping, UI fan-out, replay, and the offline training pipeline. This gives you
the stream's benefits (replay, multiple independent consumers, backpressure isolation) without
putting an enqueue/dequeue round trip inside a 25ms deadline.

**Why the async work is a separate goroutine pool with a bounded queue, not fire-and-forget**
(fixes F-05): fire-and-forget contends with the hot path and has no backpressure. A bounded pool
with an explicit overflow policy (`drop-to-WAL-only`, alarm, and `degraded=["async_backlog"]` on
subsequent decisions) makes the failure visible and survivable.

---

## 5 — Service decomposition

Five deployables. The split is by **failure domain and latency class**, not by layer.

| Service | Language | Why separate | Scales on |
|---|---|---|---|
| **decision** | Go | Hard deadline. Nothing else may share its scheduler or its heap | RPS (stateless, N replicas) |
| **profile-apply** | Go | Writes to the profile store. Separated so a write stall cannot stall a read | Event rate |
| **graph** | Go | Long, bursty, unbounded work (component maintenance, BFS). Must never share a scheduler with a 25ms deadline (fixes F-29) | Edge rate, memory |
| **casework** | Go or Python | Analyst-facing. Latency class is "seconds". Different SLO entirely | Analyst count |
| **ml-platform** | Python | Training, backtesting, calibration, drift, red-team analysis. **Never on a request path** | Offline |

Plus: **console** (React/TS) and **fanout** (Go, SSE) — the fan-out is its own process specifically
so that broadcast serialisation cannot touch the scoring heap (fixes F-68).

`[P0]` note: you may run `decision`, `profile-apply`, `graph`, and `casework` as one binary with
four goroutine pools and a build flag. The *interfaces* must be the split ones from day one so that
separating them later is a deployment change, not a rewrite. This is §3.2 applied.

---

## 6 — Technology choices

Each row states the rejected alternative and the condition under which the choice flips — which is
§3 applied to the stack itself.

| Component | Choice | Rationale | Rejected | Flip when |
|---|---|---|---|---|
| **Decision service** | **Go** | Predictable tails without a GC pause budget you cannot control; sub-ms GC with `GOGC` + `GOMEMLIMIT`; excellent Redis/PG clients; team velocity far above Rust | **Python/asyncio** — a GIL and a GC in a p99.9 path, and the previous design's own latency budget was 100× off partly because nobody measured it (F-04). **Rust** — right answer for P2, wrong cost at P0/P1 | If p99.9 > 30ms after tuning, or if you reach P2. The `Signal`/`Scorer` seams mean the port is one service, not the system |
| **Model inference** | **LightGBM compiled via Treelite/TL2cgen → shared object, called via cgo**; or `leaves` (pure Go) | Single-row predict in **2–50 µs** for a 300-tree model. Removes the largest error in the previous budget | Python in-process (F-04); a model server over HTTP (adds an RTT to a 25ms budget) | Never for GBMs. If you adopt a NN, move to ONNX Runtime |
| **Attribution** | **TreeSHAP** (`pred_contrib`), exact, ~0.2–1 ms | Exact per-transaction signed contributions, which is what the alert screen claims to show. Replaces the entire hand-built log-odds composition (F-10) | Global feature importance (wrong thing); LIME (slow, unstable) | If SHAP exceeds budget, compute it in the async lane and render it a beat later |
| **Profile store** | **Redis 7 Cluster**, hash-tagged keys, Redis Functions for read-modify-return | Sorted sets + hashes + server-side Lua give exactly the primitives needed; ubiquitous ops knowledge | Aerospike/Dragonfly (better at P2, more ops cost at P0); Postgres (cannot hold the read budget) | At P2, or when memory sizing (02-DATA §3.6) exceeds what Redis economics tolerate |
| **Event log** | **Redis Streams `[P0]` → Redpanda `[P1]`** | Streams are free at P0 and consumer-group-shaped, so the migration is a client swap. Redpanda for durability + replay + independent consumer lag at P1 | Kafka (JVM ops tax at this size); Streams at P1 (no tiered storage, memory-bound retention) | When retention needs exceed RAM, or when you need > 3 independent consumer groups |
| **System of record** | **Postgres 16**, time-partitioned | Everything: transactions, decisions, cases, labels, audit. Partitioning by day makes retention and vacuum tractable | Timescale (adds an extension for one query pattern); Mongo (no) | If decision volume at P1 makes single-writer partitions hurt, shard by `bank_instance` then by `decision_shard` |
| **Rule evaluation** | **CEL (`cel-go`)**, compiled once at bundle load | A real, sandboxed, typed expression language with a published grammar. Kills the eval-RCE path (F-72) | Python `eval` (RCE); a bespoke parser (you will get it wrong) | Never |
| **UI transport** | **SSE from a dedicated `fanout` service**, coalesced + sampled | One-way, auto-reconnecting, proxy-friendly, and — critically — **not in the scoring process** (F-68) | WebSocket from the scorer (the previous choice, made to "own the latency number", which is exactly what it endangered) | If you need client→server streaming, which you do not |
| **Console** | React + TS + Tailwind; Recharts for standard charts; Canvas for the link graph | Unchanged from the previous plan; it was right | — | — |
| **ML platform** | Python: LightGBM, statsmodels, Polars/DuckDB for backtests | Correct home for Python. Offline, no latency contract | — | — |
| **LLM** | **None on the request path.** Templated narrative default; local model behind a flag, structured input only | Carried forward from the previous design, which was right | Hosted API — contradicts the DPDP argument | Never |

**Honest note on Go vs Rust vs Python.** The previous stack (Python end-to-end) was chosen so "the
model and the API share a process." That is a real benefit and it is worth roughly 5–10 ms of p99 —
which is 20–40% of the entire deadline. With Treelite the model and the API still share a process,
in Go, at 50 µs. The benefit is retained and the cost is not paid. If your team cannot write Go,
build P0 in Python **behind these exact interfaces**, state the profile honestly, and name the port
as the P1 step. That is a defensible answer; "Python scales fine" is not.

---

## 7 — Trust boundaries

The previous design had no authentication anywhere (F-71). Five boundaries, each with an explicit
control.

```
   ┌──────────────────────────────────────────────────────────────────┐
   │ B1  Payment switch → decision            mTLS, client-cert pinned│
   │     Untrusted content: every event field is attacker-influenced  │
   ├──────────────────────────────────────────────────────────────────┤
   │ B2  Analyst / operator → API             OIDC + RBAC + audit     │
   │     Four-eyes required on: policy, rules, model pin, blocklist   │
   ├──────────────────────────────────────────────────────────────────┤
   │ B3  Decision service → data stores       mTLS, least-privilege   │
   │     decision svc has NO write grant on `decisions` (WAL only)    │
   ├──────────────────────────────────────────────────────────────────┤
   │ B4  Nazar → consortium registry          mTLS + per-member sig   │
   │     Everything inbound is an untrusted foreign claim (advisory)  │
   ├──────────────────────────────────────────────────────────────────┤
   │ B5  Structured findings → LLM lane       structured objects only │
   │     Raw remittance text NEVER crosses. Contradiction firewall    │
   └──────────────────────────────────────────────────────────────────┘
```

**B1 is the one people get wrong.** Every field in the incoming event is authored or influenced by
the party being scored. `device_first_seen` and `creditor_account_age_days` appear in the previous
event schema as *client-asserted* fields feeding ATO and APP features (F-43, E9 in the review).
**Server-derived values always win**, and the event's version is recorded separately as
`claimed_*` for the fraud-in-the-claim signal it sometimes carries.

**Roles for B2:**

| Role | May |
|---|---|
| `viewer` | Read cases, decisions, entities. No PII beyond masked identifiers |
| `analyst` | + dispose cases, request recovery |
| `supervisor` | + confirm blocklist additions, approve consortium publishes |
| `risk_owner` | + propose policy/rule/model changes (proposal only) |
| `approver` | + approve another principal's proposal. **Cannot approve their own** |
| `operator` | + operational flags (signal on/shadow/off), no thresholds |

Every mutation writes to the same hash-chained audit log as decisions, with the actor, the
before/after bundle version, and the approval pair. The previous design audited decisions and not
the configuration changes that produced them — which is the half that matters in an investigation.

---

## 8 — Failure domains

| Domain | Blast radius | Contained by |
|---|---|---|
| Profile store partition/outage | Feature quality, not availability | Local rails + last-known-good profile cache + value caps ([04-DECISION §6](04-DECISION-POLICY.md#6--degradation)) |
| Model artefact bad/corrupt | Score quality | Signed bundle + startup validation against golden vectors + instant registry rollback |
| Graph service down or lagging | Graph features only | Features → `NOT_EVALUATED` (not `NaN`, not zero), ring signals suppressed, decision proceeds |
| Consortium unreachable | Advisory only | Fail-open by construction — advisories cannot lower friction, so their absence cannot raise risk |
| Postgres down | Persistence, not decisions | Local WAL absorbs; decisions continue; chain reconciles on recovery ([01-LATENCY §6](01-LATENCY-RESILIENCE.md#6--durability-and-the-audit-chain)) |
| Async lane saturated | Freshness of graph/cases/UI | Bounded queue + explicit shed + `degraded` flag on subsequent decisions |
| Decision service overload | Latency | Admission control → shed to rails-only, which is **still a real decision** ([01-LATENCY §4](01-LATENCY-RESILIENCE.md#4--overload)) |
| A signal is discovered to be wrong | Whatever it touched | Drop to `shadow` at runtime (§3.3), no deploy |

**The invariant across all of them:** no failure domain can produce a `BLOCK` that would not have
occurred healthy (D7), and every degraded decision is tagged, persisted, and replayable.

---

## 9 — Repository layout

```
nazar/
├── docs/                         # these documents + docs/adr/
├── proto/                        # canonical event, decision, wire — the Type 1 contracts
│   ├── event.v1.proto            #   additive-only; readers ignore unknown fields
│   ├── decision.v1.proto
│   └── consortium.v1.proto
│
├── go/
│   ├── cmd/{decision,profile-apply,graph,casework,fanout}/
│   ├── internal/
│   │   ├── contract/             # generated types + ScoringContext + Finding
│   │   ├── profile/              # ProfileStore seam: keys, functions, bundle, fakes
│   │   ├── features/             # pure functions. NO imports of profile or redis
│   │   ├── signals/              # Signal seam: rules(CEL), model, graph, consortium, novelty
│   │   ├── scoring/              # Scorer + Calibrator seams; treelite binding
│   │   ├── decide/               # rails, expected-cost engine, ladder, advisory
│   │   ├── wal/                  # local durable append + shipper
│   │   ├── audit/                # per-shard chain, Merkle anchor
│   │   ├── consortium/           # tokens, OPRF client, registry client, reputation
│   │   └── obs/                  # metrics, tracing, deadline propagation
│   └── test/
│       ├── invariants/           # the property tests — see 06-BUILD-PLAN §4
│       └── golden/               # feature vectors + expected decisions, checked in
│
├── py/
│   ├── generator/                # population, behaviour, typologies, warmup
│   ├── training/                 # feature spec, train, calibrate, bundle, sign
│   ├── eval/                     # backtest, per-typology, calibration, off-policy
│   ├── drift/
│   └── redteam/
│
├── sql/migrations/
├── policy/                       # versioned bundles: rails, thresholds, segments, ladder caps
├── rules/                        # versioned CEL rule bundles
└── console/
```

**Two structural rules that keep the seams honest:**

1. `internal/features` may not import `internal/profile` or any client library. Features are pure
   functions of `(ProfileBundle, Event)`. Enforced by a lint rule. This is what makes the entire
   feature layer testable from checked-in golden files and what would have caught F-34.
2. `proto/` changes require an ADR. Nothing else does.

---

## 10 — Configuration and policy

Four versioned bundles, each independently rollable, each signed, each stamped on every decision.

```yaml
# policy/2026-08-14.001.yaml
version: "2026-08-14.001"
effective_from: "2026-08-14T00:00:00Z"
approved_by: ["risk.owner@bank", "approver@bank"]     # four-eyes, both required

regulatory_rails:            # absolute by law. Each carries its authority and date.
  npci_new_beneficiary_cooling:
    authority: "NPCI UPI beneficiary cooling period"
    verified_on: "2026-08-14"          # ← re-verify before every release
    applies_to: [UPI]
    predicate: pair.first_added_within_hours < 24     # (payer,payee), NOT payee age — fixes F-43
    action: CAP
    cap_minor: 500000                                  # ₹5,000 in paise

policy_rails:                # ours. Never BLOCK. Named separately from regulatory.
  extreme_velocity:
    predicate: payer.txn_count_1h > payer.baseline_txn_1h_p999 * 3
    action: STEP_UP_INTERSTITIAL       # NOT block — fixes F-42 / D7

economics:
  loss_given_fraud: {UPI: 0.95, IMPS: 0.95, NEFT: 0.5, CARD_CNP: 0.2}
  friction_cost:                        # the term the previous engine omitted — F-48
    step_up_minor: 1200
    interstitial_minor: 4000
    hold_minor: 90000
    false_block_minor: 250000
  segments:                             # per-cell operating points — implements §21.5 properly
    - match: {tenure_band: ">2y", rail: UPI, amount_band: "<5k"}
      operating_point: {target_challenge_rate: 0.004}

ladder:
  rungs: [ALLOW, ALLOW_MONITOR, STEP_UP, STEP_UP_INTERSTITIAL, HOLD]
  advisory_max_rung: STEP_UP_INTERSTITIAL   # advisories can NEVER reach HOLD — fixes F-20
  advisory_max_steps: 2

control_group:
  fraction: 0.005                       # unbiased training data — fixes F-12
  exempt: [regulatory_rails, local_confirmed_blocklist]
```

Every decision record carries `policy_version`, `rules_version`, `model_bundle_version`,
`signal_registry_version`. Any decision, at any time, can be re-explained with the exact
configuration that produced it — and any two configurations can be compared with a `GROUP BY`.

---

**Next:** [01-LATENCY-RESILIENCE.md](01-LATENCY-RESILIENCE.md)
