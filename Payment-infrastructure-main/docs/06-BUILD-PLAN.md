# 06 — Build Plan

**The rule for this document: the architecture is the deliverable, the code is the proof.**
Build the thinnest thing that demonstrates each architectural claim, then stop. Every milestone
below names what to build **and what to deliberately stub** — a stub you chose and labelled is an
architectural decision; a stub you drifted into is debt.

---

## 1 — Scope discipline

Three lists. Keep them visible; re-read them when you feel the urge to polish.

### Build properly (these ARE the architecture)

| # | Thing | Because |
|---|---|---|
| 1 | The five seams ([00-ARCH §3.2](00-ARCHITECTURE.md#32--ports-and-adapters-with-the-seams-named-up-front)) | Every "we could swap that" claim depends on them existing |
| 2 | Canonical event + feature registry + decision record | The Type 1 contracts. Wrong here = rewrite |
| 3 | Point-in-time feature read (read-before-write, `ZCOUNT` not `ZCARD`) | The correctness core. Get it wrong and every number downstream is wrong |
| 4 | Rails / rule-features split | Deletes a whole subsystem and makes the calibration answer true |
| 5 | Expected-cost decision + policy bundle versioning | The product |
| 6 | Degradation + the timeout action | The resilience story, and it's ~100 lines |
| 7 | The invariant test suite (§4) | It's what makes every claim checkable rather than asserted |

### Stub deliberately, label loudly

| Thing | Stub | Why it's fine |
|---|---|---|
| Consortium OPRF | Shared-pepper HMAC with `ep` epoch field | The *interface* proves the design; the crypto upgrade is a day, named in the ADR |
| Graph store | In-process Go adjacency, decay + component cap implemented | Correct algorithm at `[P0]` scale; sharding is a `[P1]` concern |
| Off-policy evaluation | Log `action_propensity`; don't build the IPW estimator | Logging is the hard part to retrofit; the estimator is a notebook |
| Recovery workflow | Status column + one manual transition | Proves the data model; the integrations don't exist to build against |
| Bank B instance | Same binary, different `bank_instance`, separate Redis DB | Two processes is a config file, not a system |
| Card rail model | Train on generator with the card schema | Proves per-rail models work. Say `[RECOVERED]` |
| Segmented operating points | Two segments, not the full cross product | Proves the mechanism |

### Do not build (name these on a slide instead)

Sanctions/AML screening · 3DS/ACS integration · issuer–acquirer reason-code interchange · dispute
case management · RBI grievance-redressal loop · sequence models for slow drip · GNNs · federated
*learning* · blockchain for the ledger · behavioural biometrics · canvas fingerprinting · real-time
retraining.

The previous design's exclusion table (`§20.9`) is the best single slide in it. Keep it, with the
reasons, not the conclusions.

---

## 2 — Build order

**Vertical slices, not horizontal layers.** The previous plan built layer by layer, which means you
find out whether the system works near the end. Slice 1 is one typology end-to-end; everything after
is addition.

### Milestone 0 — Skeleton (½ day)

`proto/` contracts · the five interfaces with a fake implementation each · Postgres migrations ·
one Go binary, four goroutine pools · `/healthz` · metrics.

> **Gate:** `POST /v1/decide` returns a hardcoded `ALLOW` in < 1 ms, persisted, chained, visible on
> the SSE stream. **Nothing real yet, and that's correct — the shape is the point.**

### Milestone 1 — The APP-scam slice, end to end (2 days)

Generator: 2k accounts, 90-day warm-up, **one typology (mule fan-out)** · profile store with the
payer/payee groups · ~15 features · 3 rails + 5 rule-features · expected-cost decision · WAL +
persist · one console screen.

> **Gate:** a generated APP-scam transaction gets `STEP_UP_INTERSTITIAL` with correct reason codes
> in < 10 ms p99, and `test_window_arithmetic_property` +
> `test_feature_catalogue_key_coverage` pass.
>
> **This gate is the whole project.** Everything after it is amplification. If it slips, cut
> typologies, not this.

### Milestone 2 — Model + calibration (1.5 days)

Train on the generator · time-forward 3-way split · beta calibration on a natural-prevalence slice ·
prevalence correction · monotone constraints · TreeSHAP · Treelite bundle + golden vectors ·
registry with shadow.

> **Gate:** reliability diagram + ECE on the governance screen; the prevalence slider moves the
> challenge rate; a bad bundle is refused at load.

### Milestone 3 — Graph + rings (1.5 days)

Edges · frequency-derived weights (§2 of [05](05-GRAPH-CONSORTIUM.md)) · decay + component cap ·
`ring_score` as a feature · bounded `hops_to_cashout` in the graph pool · money-flow view.

> **Gate:** `test_merchant_is_not_a_ring` passes — 500 payers to one merchant produces zero ring
> signal — **and** an 11-payer 3-day-old forwarding beneficiary does. That single test pair is the
> answer to the hardest question this layer will get.

### Milestone 4 — Cases, workbench, Time Machine (2 days)

Grouping by entity/component · exposure-ordered queue with SLA · alert detail (SHAP bars, baseline
chart, beneficiary card, counterfactual) · templated narrative · Time Machine as a read of persisted
features.

> **Gate:** the case exists before anyone clicks; Time Machine output byte-matches the persisted
> vector.

### Milestone 5 — Consortium, two instances (1 day)

Tokens with epoch · report/retract/dispute/expire · per-reporter chains + Merkle root · legal-entity
independence · local filter + exact confirm · `GET /v1/federation/wire/{id}`.

> **Gate:** Bank A confirms → Bank B blocks a payee it has never seen; the wire payload is on
> screen; `test_single_reporter_cannot_block` and `test_two_bins_one_bank_is_one_reporter` pass.

### Milestone 6 — Red team + novelty (1 day)

Remaining typologies · attack console with evasion knobs · evasion **search** · leaf-space conformal
novelty in `shadow` · unknown-pattern queue.

> **Gate:** the matrix fills live; novelty shows a conformal p-value with a true explanation; a
> held-out typology routes to the queue.

### Milestone 7 — Resilience, governance, chaos (1 day)

Degradation ladder · load + chaos tests in CI · drift panel · feature-integrity panel · label
maturity · config audit chain · RBAC.

> **Gate:** `prop_no_block_under_degradation` and `prop_deadline_always_answered` pass; killing
> Redis live degrades visibly and recovers.

### Milestone 8 — LLM lane (½ day)

Hosted Claude API narrative writer with structured output · the firewall · the injection fixture ·
investigation agent over read-only tools.

> **Gate:** `test_remittance_injection_never_reaches_the_narrator` passes; killing the API key falls
> back to the template with no visible break.

### Milestone 9 — Presentation (2 days, non-negotiable)

Visual system · screen inventory · keyboard-driven acts · safe mode (recorded session through the
**real** UI) · projector rehearsal · **five timed runs**.

> The previous plan names UI as *"the largest remaining gap"* while also naming presentation as the
> top priority. Do not let this compress.

**~13 days.** Milestones 1 and 9 are the ones that must not slip.

---

## 3 — Where the effort goes

```
  Architecture + contracts   ████░░░░░░░░░░░░░░░░   15%   ← the deliverable
  Correctness core (M1–M2)   ████████░░░░░░░░░░░░   30%   ← where bugs are expensive
  Breadth (M3–M6)            ██████░░░░░░░░░░░░░░   25%   ← where "good enough" is genuinely enough
  Resilience + tests (M7)    ███░░░░░░░░░░░░░░░░░   10%
  Presentation (M9)          █████░░░░░░░░░░░░░░░   20%   ← where judges spend their attention
```

**The trap to avoid:** perfecting M3–M6. Breadth exists to prove the architecture admits those
layers. A ring detector that handles the merchant case correctly and nothing else is worth more than
one that handles nine cases and blocks merchants.

---

## 4 — The invariant suite

Twelve property tests. They are the architecture in executable form, and they're the cheapest
credibility in the project.

| Test | Asserts |
|---|---|
| `prop_no_block_under_degradation` | Every injected failure × 10k events: no `BLOCK` that wouldn't occur healthy (D7) |
| `prop_advisory_monotone_and_capped` | Advisory never lowers the rung, never exceeds `advisory_max_rung` (F-20) |
| `prop_deadline_always_answered` | Under any injected latency, a decision returns within the deadline |
| `test_window_arithmetic_property` | Redis window reads match a brute-force reference over random streams (F-32/33) |
| `test_feature_catalogue_key_coverage` | Every registry feature has a backing key and a producer (F-34). ~30 lines |
| `test_no_io_after_profile_load` | Connection spy: zero Redis/PG calls in scoring or decision |
| `test_replay_is_a_read` | Time Machine output == persisted vector, byte-for-byte |
| `test_training_query_is_point_in_time` | No training path omits `available_at > decided_at` (F-57) |
| `test_no_feature_derives_from_our_decisions` | Registry provenance check (D6) |
| `test_merchant_is_not_a_ring` | 500 payers → one merchant → zero ring signal (F-26) |
| `test_two_bins_one_bank_is_one_reporter` | Legal-entity collapse on the ≥2 rail (F-53) |
| `test_remittance_injection_never_reaches_the_narrator` | Attacker text never crosses B5 |
| `test_finding_without_explanation_raises` | D4, at construction |
| `test_signal_that_did_not_run_is_not_clean` | D5, four-state status |

The previous design's *"demo beats are tests"* idea is excellent and is kept — these run **under**
those, and they're what makes the golden rules real rather than a table.

---

## 5 — Claims register

**Print this. Read it before rehearsal.** Every claim, its tier, and what it rests on. A claim not on
this list does not get said.

| Claim | Tier | Rests on | Say it like |
|---|---|---|---|
| p99 decision latency | `[MEASURED] [P0]` | Load test at stated RPS | "9.4 ms p99 at 180 RPS, n=2.1M — total, not service time" |
| Model architecture quality | `[MEASURED]` | IEEE-CIS/ULB time-split | "PR-AUC on real labelled fraud; validates the pipeline, doesn't score your payment" |
| Per-typology detection | `[RECOVERED]` | Generator, held-out params | "Recovers N% of what our generator produced. That's pipeline validation, not a detection rate" |
| Challenge rate / value-recall | `[MODELLED]` | Assumed prevalence | "At an assumed 0.05% prevalence — here's the slider" |
| Calibration | `[MEASURED]` | ECE on natural-prevalence slice | "Reliability diagram, ECE 0.02" |
| Scaling | `[MODELLED]` | The sizing model | "285 GB across 6 shards at P1. The pair keyspace is the binding constraint" |
| Consortium privacy | **Depends** | §4.1 option | Option A: "pseudonym, not confidentiality" · Option B: "OPRF — members can't enumerate" |
| Regulatory framing | **Unverified** | Re-check primary sources | Verify DPIP + the RBI circular the week of the pitch |

### Sentences to delete from the deck

| Don't say | Say instead |
|---|---|
| "94% of fraud detected" | "Recovers 94% of generated mule fan-out — that's the pipeline, not the world" |
| "Non-invertible tokens" | "Pseudonyms. The operator can't invert; members can" (or ship the OPRF) |
| "Nazar decides 700M times a day in 38ms" | "At P0 we measure 9.4 ms p99. At P1 we've modelled it — here's the bottleneck" |
| "This account was created 3 days ago" | "We first saw this account 3 days ago" |
| "Worst case a false report costs three seconds" | "Advisories are capped below hold — worst case, one extra confirmation screen" |
| "MuleHunter reported ~95% accuracy" | Cite the deployment, not the metric — accuracy is meaningless at that base rate |
| "Averaging above 5,000 TPS" | "8,660 TPS average, ~30k peak" |

---

## 6 — Out of scope

Stated so the gaps are chosen rather than missed — the discipline the previous design got right.

**Not built:** sanctions/AML screening (a separate regulated system with its own false-positive
economics) · 3DS/ACS · issuer–acquirer reason-code interchange · dispute case management · RBI
grievance redressal (though [05 §4.2](05-GRAPH-CONSORTIUM.md#42--revocation-expiry-dispute) builds
the *mechanism* it would need) · sequence models for slow drip · GNNs · federated learning ·
entity resolution across accounts.

**Known-weak, and say so:** slow drip. Patient extraction produces no velocity signal, no amount
outlier, no new-payee event, and no graph structure, because each transaction is genuinely
unremarkable in isolation. The fix is sequence models over account history rather than point-in-time
features. **Show the failure** — a team that walks to its own weakest point removes the entire attack
surface of Q&A. That was the single best paragraph in the previous playbook and it survives intact.

---

## 7 — ADRs to write (one page each, with a revisit trigger)

| ADR | Decision |
|---|---|
| 001 | Synchronous advice service, not stream-driven — and the caller's timeout action |
| 002 | Go for the decision path; Python offline. When to reconsider |
| 003 | Rails-vs-features split; why the log-odds composition layer was deleted |
| 004 | Novelty via leaf-space conformal; ships in shadow. **Revisit when:** precision measured on ≥500 matured labels |
| 005 | Ring weight as a frequency function, not a constant — why the ported table didn't transfer |
| 006 | Consortium: pseudonymisation vs OPRF; the ≥2 rail's independence model |
| 007 | Generator for validation, never for quoted metrics — the three claim tiers |
| 008 | Hosted LLM at P0 (synthetic data), in-perimeter at P1, same interface |
| 009 | Where a distributed ledger belongs and where it does not |

ADR-009 is the blockchain-scoping slide in document form. Having written it *before* being asked is
the difference between restraint and retreat.

---

**Back to:** [00-ARCHITECTURE.md](00-ARCHITECTURE.md) · [REVIEW.md](../REVIEW.md)
