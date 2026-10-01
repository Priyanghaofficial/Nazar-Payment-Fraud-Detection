# 07 — Architecture Optimised for Presentation

**The thesis: an architecture optimised for presentation is one where every invariant has a visible
artifact.** Not decoration bolted on afterwards — the same sound design, with the *observability of
its own guarantees* treated as a first-class requirement.

The previous documents named the UI as *"the largest remaining gap"* while also naming presentation
as the top priority. This closes it, and it does so by making presentation a property of the
architecture rather than a layer on top.

---

## 1 — The rule: every claim renders

For each architectural claim, there is a thing on screen that *is* the evidence. If a claim has no
artifact, either build the artifact or stop making the claim.

| Architectural claim | Visible artifact | Cost to build |
|---|---|---|
| Decisions are tamper-evident | **Chain viewer**: seq, prev_hash, hash, Merkle root, "verify chain" button that recomputes live | ~half a day |
| Features are materialised, not recomputed | **Time Machine**: judge picks any transaction, sees the exact stored vector + staleness per source | Free — it's a `SELECT` |
| A check that didn't run isn't clean | **Four-state chips**: ✓ clear · ▲ fired · — n/a on IMPS · ○ not evaluated (with reason) | Free — the data model already carries it |
| Signals are hot-swappable | **Lane strip**: 5 lamps, `off / shadow / live`, toggleable on stage | ~1 hour |
| The score is calibrated | **Reliability diagram** + ECE, and the **prevalence slider** that moves the challenge rate live | ~2 hours |
| Rules don't double-count | **SHAP bars** that sum exactly to the score, with rule-features shown as ordinary features | Free — `pred_contrib` |
| Degradation never blocks | **Kill Redis on stage.** Banner, lanes dim, value cap appears, window replays on recovery | ~2 hours |
| Only a hash crosses the wire | **`GET /v1/federation/wire/{id}`** rendered as raw bytes | ~10 lines |
| Streaming features are correct | **Feature integrity panel**: streamed vs. batch, divergence rate per feature | ~40 lines |
| The graph doesn't flag merchants | **Weight inspector**: pick any beneficiary, see its computed edge weight and why | ~2 hours |
| Latency is honest | **Three numbers**, always: queue delay · service time · total, p50/p99/p99.9 | Free |

**Everything in that table is either free or under half a day, because the architecture already
produces the data.** That is what "optimised for presentation" means here: the decision records,
four-state statuses, staleness stamps, propensities, and version stamps were chosen partly *because
they render*.

---

## 2 — Six architectural choices made for legibility

Each is defensible on engineering grounds alone — that's the constraint — and each was chosen over
an equally sound alternative because it shows better.

**1. Three latency numbers, not one.** Publishing `queue_delay / service / total` separately is
correct engineering ([01-LATENCY §1](01-LATENCY-RESILIENCE.md#1--how-latency-is-measured)) *and* it
is the answer to "is that number real?" — you show the one that includes everything and explain why
the other two exist. A single number invites the question; three answer it pre-emptively.

**2. Four-state signal status.** `NOT_EVALUATED` vs `NOT_APPLICABLE` vs `CLEAR` is correct (D5) and
it renders as three visibly different chips. **The screen judges look at longest becomes the screen
that demonstrates the deepest idea in the system.** Kept verbatim from the previous design — it was
its single best idea.

**3. Rails computed with zero I/O.** Required for the deadline guarantee
([01-LATENCY §2](01-LATENCY-RESILIENCE.md#2--the-deadline-and-the-timeout-action)) — and it means
you can unplug Redis on stage and the system keeps deciding. **Resilience you can perform beats
resilience you claim.**

**4. Per-shard chains + Merkle anchor.** The correct answer to N stateless writers
([01-LATENCY §6.2](01-LATENCY-RESILIENCE.md#62--per-shard-chains-cross-shard-merkle-anchor)) — and
it renders as a tree with a verify button, which is far more legible than a single opaque hash
column.

**5. Prevalence as an explicit slider.** Mathematically required (F-07) — and it converts the
weakest number in the previous deck into the best interaction in this one. A bank judge substitutes
their own prevalence and watches every downstream number move.

**6. Signal registry as runtime config.** The evolvability mechanism
([00-ARCH §3.3](00-ARCHITECTURE.md#33--change-without-redeploy-the-four-dials)) — and it means a
judge can say *"turn the model off"* and you do, and the deterministic backbone keeps working. **A
demo with the model disabled becomes a feature, not a failure.**

---

## 3 — Screen inventory

Seven screens. Bloomberg terminal, not consumer dashboard: dense, calm, instrument-like. Tabular
numerals everywhere — a metric that jitters looks broken even when it's right.

```
┌─ S1 OPS CONSOLE ───────────────────────────────────────────────────────┐
│ ┌ live decisions ─────────────────┐ ┌ lanes ────┐ ┌ latency ─────────┐ │
│ │ ts   payer→payee   ₹   action   │ │ ●rules    │ │ queue   0.1 ms   │ │
│ │ (coalesced, sampled, ~10/s max) │ │ ●model    │ │ service 0.6 ms   │ │
│ │                                 │ │ ◐novelty  │ │ TOTAL   0.7 ms   │ │
│ │                                 │ │ ●graph    │ │ p99     9.4 ms   │ │
│ │                                 │ │ ●consort. │ │ p99.9  21.0 ms   │ │
│ └─────────────────────────────────┘ └───────────┘ └──────────────────┘ │
│ ┌ narrator ──────────────────────────────────────────────────────────┐ │
│ │ ₹49,999 → beneficiary first seen 3d ago · fan-in 11 · fwd 94%/41s  │ │
│ └────────────────────────────────────────────────────────────────────┘ │
│ value prevented ₹4.2L │ challenge 1.4% │ degraded: none │ policy .001  │
└────────────────────────────────────────────────────────────────────────┘
```

| # | Screen | Carries |
|---|---|---|
| **S1** | Ops console | Live decisions (coalesced), lane strip, three latency numbers, narrator line, metrics strip |
| **S2** | **Alert detail** ⟵ *the design budget goes here* | SHAP bars · payer's 30-day baseline with this txn marked · beneficiary card · four-state chip row · counterfactual · narrative · **staleness + version footer** |
| **S3** | Case queue | Exposure-ordered, SLA countdown, ring badge, typology |
| **S4** | Graph / money flow | Link graph · victims→layering→exit · **weight inspector** |
| **S5** | Governance | Registry · shadow agreement · reliability diagram + ECE · drift · **feature integrity** · label maturity · **chain viewer** · config audit |
| **S6** | Red team | Typology matrix filling live · evasion search · unknown-pattern queue |
| **S7** | Policy | Cost curve + operating point · **prevalence slider** · per-segment challenge rate |

### S2 is the whole product

The screen judges look at longest. Its footer is what separates this from a dashboard:

```
─────────────────────────────────────────────────────────────────────
model upi-v1.4  policy 2026-08-14.001  rules .003  registry .002
features fresh: payer 0.2s · payee 0.4s · graph 3.1s ⚠ · consortium 1.1s
degraded: none      decided 0.68 ms      chain #40219 ✓
─────────────────────────────────────────────────────────────────────
```

Four claims in one strip: reproducibility, D2 staleness honesty, degradation state, and chain
integrity. **Every one of those fields already exists in the decision record** — see
[02-DATA §7](02-DATA-AND-FEATURES.md#7--postgres-schema).

---

## 4 — Visual system

| | |
|---|---|
| **Palette** | Neutral graphite ground. **Semantic colour reserved strictly for risk state** — green/amber/red mean allow/step-up/block and nothing else |
| **The accent problem** | Because green/amber/red are semantic, the trust accent must sit **outside the risk ramp** — which rules out the obvious fintech choices. **Deep indigo** (`#4C5BD4`) or **slate teal** (`#2A7B76`). This is a real constraint the previous design correctly identified and left open |
| **Numerals** | `font-variant-numeric: tabular-nums`, fixed-width slots |
| **Density** | Asymmetric: the decision dominates, evidence packs tightly around it |
| **Banned** | Cyan, purple, gradient heroes. That palette is the "AI dashboard" tell |
| **Projector** | Rehearse on the real projector. Projectors crush dark greys — a dark theme tuned on a laptop becomes a black rectangle |
| **Copy** | Human banking language customer-facing, engineering language console-only. The interstitial says *"we first saw this account 3 days ago"*, never *"advisory escalation"* |
| **Never** | A green "Verified" badge on an allowed payment. The console says `ALLOWED`; the metric is `value_recall`, not "accuracy". **Absence of evidence is not evidence of absence** |
| **No fabricated data** | Every number traces to real backend output. A placeholder sparkline is a lie judges can catch |

---

## 5 — The demo, mapped to architecture

Each act exists to make one architectural property visible. If an act doesn't, cut it.

| Act | Beat | Proves |
|---|---|---|
| **0** (0:00) | Already running. Don't open a laptop | It's a system, not a script |
| **1** (0:15) | Judge pays ₹120 → green, latency shown. Then the KYC scam QR → **interstitial** → *they choose* | Expected-cost decisioning; the warning is real ([04 §7](04-DECISION-POLICY.md#7--act-1-fixed) Option A); case opens **on override** |
| **2** (1:15) | S2 already open. SHAP bars · baseline · beneficiary card · **four-state chips** · counterfactual · footer | Attribution, D5, reproducibility. *"The analyst didn't reconstruct any of this. It was written before they clicked"* |
| **3** (2:15) | Graph → **weight inspector on a merchant → 0.00**, on the mule → signal. Confirm Fraud → four effects animate | The graph doesn't flag merchants — **the objection, answered before it's raised** |
| **4** (3:00) | Bank B blocks on first contact. **Wire payload on screen** | Consortium. Say "pseudonym, not confidentiality" — [05 §4.1](05-GRAPH-CONSORTIUM.md#41--the-token-claim-corrected) |
| **5** (3:30) | Red team fires. Matrix fills. Novel typology → conformal p-value → queue. **Then slow drip fails. Do not skip** | Breadth, honest novelty framing, and the failure that removes the Q&A attack surface |
| **6** (4:15) | **Kill Redis.** Banner, lanes dim, value cap, still deciding. Restore → window replays | D7, performed |
| **7** (4:45) | **Prevalence slider.** Then the cost curve. Then the chain viewer → verify | Calibration is an assumption you can move; the log is checkable |

**Act 6 is new and is the strongest engineering beat available.** Thirty seconds, and no other team
will unplug their own datastore on stage.

**Act 3's weight inspector is the second strongest**, because it answers the hardest question about
the graph layer *before anyone asks it* — and the previous design would have failed that question
badly.

### Production craft

Zero menu clicks — every act is a keyboard shortcut, rehearsed until your hands know them. No
spinner is ever seen; 90 days pre-warmed. One alert tone, used exactly twice. Safe mode: a recorded
session replayed through the **real** UI. **Five full timed runs minimum**, Act 1 flawless — practise
handing the phone over, that's the beat that goes wrong.

---

## 6 — Q&A, with the artifact to open

| Question | Answer | Open |
|---|---|---|
| "What's your accuracy?" | "Wrong metric at this base rate — 99.95% accuracy is `ALLOW` unconditionally. Here's PR-AUC on real labelled fraud, and calibration on the live model" | S5 |
| "Is that model scoring my payment?" | "No — IEEE-CIS is card-not-present e-commerce, yours is a UPI push. Different schema, different model, exactly as at a real bank" | S5 registry |
| "Where do your numbers come from?" | "Three tiers, and I'll tell you which every time. Measured on real labels, recovered from our simulator, or modelled on a stated assumption — that one's a slider" | S7 |
| "Won't the graph flag every merchant?" | "Try it" — inspect a merchant → 0.00 | S4 |
| "What if another bank falsely reports my customer?" | "Advisories are capped below hold. Worst case, one extra confirmation screen. That's a policy cap with a property test, not a promise" | S2 pre-advisory action |
| "How do I know this isn't scripted?" | "Pick any transaction on that screen" | Time Machine |
| "What if Redis dies?" | "Let's find out" | Act 6 |
| "Does it retrain when I confirm?" | "No, and I want to be precise. Propagation is immediate — blocklist, graph, consortium. Retraining is on a lag because chargebacks take 30–90 days. That counter is labels pending maturity" | S5 |
| "Is the token really private?" | "It's a pseudonym. The registry operator can't invert it; members can, because they hold the pepper and account numbers are low-entropy. That's what the consortium agreement is for. If you want members unable to enumerate, that's an OPRF — a day of work, behind this interface" | wire endpoint |
| "How does it scale?" | "Measured at P0, modelled at P1 — and the binding constraint is the pair keyspace, not the scoring. Here's the sizing" | 01-LATENCY §8 |
| "Where's the LLM?" | "Off the request path entirely. It renders findings the deterministic engine produced, never sees raw payment text, and can't state an action — that's a firewall, and here's the injection test" | test output |
| "What doesn't it do?" | The exclusion table, with reasons | slide |

**Every row opens an artifact.** That is the presentation architecture working.

---

## 7 — If everything collapses

Ship four things: **the generator, the profile store with baseline-relative rules, the APP-scam
interstitial, and S2.** That is Act 1 and Act 2 — a judge gets scammed, then sees exactly why the
system knew. Everything else amplifies those two.

The previous playbook's version of this line was right. The only change: **S2 must carry the
four-state chip row and the version/staleness footer**, because those two components are what make
it an architecture demo rather than a nice screen.

---

**Back to:** [00-ARCHITECTURE.md](00-ARCHITECTURE.md) · [06-BUILD-PLAN.md](06-BUILD-PLAN.md)
