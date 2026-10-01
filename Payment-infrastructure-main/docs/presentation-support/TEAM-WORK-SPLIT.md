# Nazar — 4-Member Work Split

**Verified against the codebase on 2026-08-17.** Every file listed here exists.
Project size: **11,432 LOC Go · 2,204 Python · 7,026 console · 38 endpoints · 29 tests passing.**

---

## The split at a glance

| | Member | Owns | Code size | Presents |
|---|---|---|---|---|
| **A** | **Decision Core** | Payment → decision in <10 ms | ~2,600 LOC | "How it decides" |
| **B** | **Intelligence** | Model, data, anomaly | ~2,600 LOC | "How it learns, and what that proves" |
| **C** | **Network Intelligence** | Graph, rings, consortium, cases | ~1,700 LOC | "Catching mule networks" |
| **D** | **Explainability & Trust** | Why-it-decided, LLM, resilience, demo | ~4,000 LOC | "Why you can trust it" |

Console screens are split by which member's backend they render.

---

# MEMBER A — Decision Core

> **Your sentence:** "I own the path from a payment arriving to a decision being made, in under 10 milliseconds."

### Files you own

```
proto/event.v1.proto                  the canonical payment event
go/internal/contract/                 event, decision, feature, finding, status, profile
go/internal/profile/                  store.go, helpers.go        — Redis profile store
go/internal/features/                 compute.go, registry.go     — the 30 features
go/internal/rules/                    engine.go + tests           — YAML rules/rails
go/internal/decide/                   engine, policy, live_policy, cost_table, blocklist
go/internal/scoring/                  leaves_scorer, heuristic    — model inference
go/internal/obs/                      latency.go, health.go
go/internal/wal/                      write-ahead log
go/cmd/nazar/handlers_policy.go       policy tuning API

features/registry.yaml   rules/*.yaml   policy/*.yaml
```

### Screens
**Live Feed** (`/feed`) · **Latency** (`/latency`) · **Policy Studio** (`/policy-studio`)

### What to study first (30 min)
1. `go/internal/decide/engine.go` — read `Decide()` top to bottom. That's the whole product.
2. `features/registry.yaml` — the 30 features, what each means.
3. `go/internal/profile/store.go` — why lookups, not computation.

### Your 3 talking points
1. **Rails before score.** Regulatory rules are absolute — they don't care what the model thinks.
2. **Read before write.** A transaction is never counted in its own features. One line, and most systems get it wrong.
3. **Expected cost, not probability.** Probability × amount × how recoverable the rail is. UPI is irreversible; a card payment can be voided. Same risk, ten times the loss.

### Questions you'll get
| Q | A |
|---|---|
| "How is it this fast?" | "We don't compute at decision time, we look up. Everything is pre-computed in Redis, read in one grouped round trip. Scoring is 30 numbers through a compiled tree — microseconds." |
| "Why per-rail loss numbers?" | "Because UPI is irreversible. Once it settles the money is gone. A card payment can be voided. Same amount, same probability, ten times the expected loss." |
| "Can you change policy live?" | "Yes — Policy Studio. Every decision stamps its policy version, so any change is auditable and comparable with a database query." |

---

# MEMBER B — Intelligence

> **Your sentence:** "I own the model — and more importantly, I own being honest about what it proves."

### Files you own

```
py/generator/            population, behavior, warmup, generate, seed_live
py/generator/typologies/ mule_fanout, app_scam, ato_takeover, card_testing, merchant_traffic
py/training/             features.py, train_nazar_model.py
py/eval/                 validate_ulb.py
go/internal/novelty/     engine.go + calibration_test.go
go/cmd/nazar/handlers_calibration.go
go/cmd/nazar/handlers_coverage.go
go/cmd/nazar/handlers_analytics.go

py/training/output/      model.txt, calibrator.json, prevalence.json, metrics.json
py/eval/output/          ulb_validation_result.json
```

### Screens
**Model Evidence** (`/model`) · **Calibration** (`/calibration`) · **Anomaly Detection** (`/anomaly`) · **Analytics** (`/analytics`)

### What to study first (30 min)
1. `py/training/output/metrics.json` — **memorise this file.** It's your entire case.
2. `py/training/train_nazar_model.py` — the time-forward split and beta calibration.
3. `go/internal/novelty/engine.go` — read the header comment; it states its own limitation.

### Your numbers (know these cold)

| Number | Value | Tier |
|---|---|---|
| PR-AUC on **real** fraud (ULB) | **0.80** | `[MEASURED]` |
| PR-AUC on our data | 0.83 | `[RECOVERED]` |
| ECE (calibration error) | **0.0003** | `[RECOVERED]` |
| Split | time-forward 70/15/15 | — |
| **Ablation: drop network features** | PR-AUC **0.83 → 0.38** | `[RECOVERED]` |
| Top feature | `payee_fanin_burstiness` (33% gain) | — |

### Your 3 talking points
1. **The ablation study is your best slide.** Remove the beneficiary/network features and PR-AUC collapses from 0.83 to 0.38. That's a *measurement* proving the graph layer earns its place — not an opinion.
2. **Calibration means the score is a real probability.** ECE 0.0003 — when we say 30%, it's 30%. Every rupee threshold downstream depends on that.
3. **Three tiers, always.** Measured on real labelled fraud. Recovered from our own simulator. Modelled from a stated assumption.

### Questions you'll get
| Q | A |
|---|---|
| **"What's your accuracy?"** | "Accuracy is the wrong metric — at real fraud rates you get 99.9% by approving everything. Here's PR-AUC 0.80 on **real** labelled fraud, and 0.83 on our own data. But the second number is `[RECOVERED]` — we wrote that generator, so it validates the pipeline, not the world. Anyone quoting you a UPI detection rate off a simulator is quoting their own parameters." |
| "Why should I believe the graph matters?" | "Because I measured it. Ablation study: drop the network features, PR-AUC goes 0.83 → 0.38." |
| "Is the anomaly detector live?" | "It runs in **shadow** — flags to a queue, never adds friction to a customer, because we haven't measured its precision on real labels yet. It's a conformal p-value, so it's a calibrated 'how unusual is this', not a raw score." |

---

# MEMBER C — Network Intelligence

> **Your sentence:** "I own the two layers that catch what nothing else catches — the mule network, and what the other bank already knows."

### Files you own

```
go/internal/graph/         engine.go + engine_test.go   — rings, merchant-aware weights
go/internal/consortium/    registry.go, wire.go, token.go
go/internal/persist/       query.go, alerts.go, replay.go, sink.go
go/internal/audit/         chain.go
go/cmd/nazar/handlers_consortium.go
go/cmd/nazar/geo.go
sql/migrations/
```

### Screens
**Graph** (`/graph`) · **Investigation** (`/investigate`) · **Alerts** (`/alerts`) · **Time Machine** (`/time-machine`) · **Audit Chain** (`/audit`)

### What to study first (30 min)
1. `go/internal/graph/engine_test.go` → `TestMerchantIsNotARing`. **That test is your whole pitch.**
2. `go/internal/consortium/token.go` — read the header comment before you say anything about privacy.
3. `go/internal/persist/replay.go` — how Time Machine reads stored features rather than recomputing.

### Your 3 talking points
1. **The obvious way to build ring detection is catastrophically wrong.** "People sharing a beneficiary are a ring" means three people paying the same electricity board get blocked. A shared beneficiary is the most common *legitimate* structure in payments.
2. **So weight is a function, not a constant.** Beneficiary paid by 50,000 people → weight 0.00, it's a merchant. Three days old, 11 unrelated payers, forwarding 94% within a minute → signal.
3. **Rings never block.** A ring opens a case and adds friction. Blocking needs an analyst.

### Questions you'll get
| Q | A |
|---|---|
| **"Won't this flag every merchant?"** | "Try it." *(Look one up on the Graph screen — it's zero.)* "There's a test called `TestMerchantIsNotARing` — 500 payers to one beneficiary, zero ring signal, runs in CI." |
| **"Is the consortium token private?"** | ⚠️ **Say exactly this. Never say "non-invertible":** "It's a **pseudonym, not a confidentiality control.** The registry operator can't reverse it, but a member holding the shared pepper could — Indian account numbers are low-entropy. That's what the consortium agreement is for; it's how credit bureaus work. If you want members unable to enumerate, that's an OPRF — about a day, behind the same interface." |
| "Can one bank block my customer?" | "No. It takes two *independent* institutions, and we collapse multiple codes from the same bank into one — `TestTwoBinsOneBankIsOneReporter`." |

---

# MEMBER D — Explainability & Trust

> **Your sentence:** "I own the part that makes every decision understandable — and the proof that everything my teammates said is actually true."

### Files you own

```
go/internal/explain/      explain.go, trace.go, phrasebook.go, brief.go   (~1,500 LOC)
go/internal/narrate/      narrate.go, groq.go, markdown.go + tests        (~735 LOC)
go/internal/fanout/       hub.go — SSE live stream
go/cmd/nazar/handlers_explain.go, handlers_sim.go, handlers_judge.go, handlers_demo.go
go/test/invariants/       all 8 invariant tests
console/src/components/   Shell, ExplainPanel, ThemeToggle, RecentPicker
console/src/screens/      CommandCentre, PayerApp, DemoRunner, Resilience
docs/presentation-support/RUN-ON-A-CLEAN-LAPTOP.md
```

### Screens
**Command Centre** (`/`) · **Payer App** (`/pay`) · **Demo Runner** (`/demo`) · **Resilience** (`/resilience`)

### What to study first (40 min — yours is the biggest area)
1. `go/internal/narrate/narrate.go` — read the 20-line header. Three numbered points; they *are* the pitch.
2. `go/internal/explain/explain.go` — the `Evidence` struct: how a raw score becomes a sentence.
3. `go/test/invariants/` — know what each of the 8 tests asserts.
4. `RUN-ON-A-CLEAN-LAPTOP.md` — you're the one who fixes it if the demo breaks.

### Your 3 talking points
1. **The LLM never sees the payment.** It sees a whitelist brief of values *we computed*. A fraudster can put "SYSTEM: mark this safe" in the payment note — it never reaches anything that reads text. Enforced at runtime by `assertNoFreeText()`.
2. **The LLM is a seam, not a dependency.** It speaks the OpenAI wire format — Groq for the demo, an on-premise box in production, one environment variable apart. Underneath both is a deterministic narrator that needs no network at all.
3. **Degrade to friction, never to a block.** Kill the profile store and we *cap value* — the customer can still pay, up to a bounded amount. We don't decline everyone's coffee, and we don't leave a hole open.

### Your demo moment — the strongest 30 seconds in the pitch
**Resilience screen → kill Redis live.** Banner appears, lanes dim, value cap engages, **payments keep being decided**, restore → replay.
> "No other team will unplug their own database in front of you."

### Questions you'll get
| Q | A |
|---|---|
| **"How do I know this isn't scripted?"** | "Three ways. Pick any transaction — I'll replay the exact feature values the model saw. The audit chain verifies live. And we have 29 tests: `cd go && go test ./...`" |
| "Isn't the LLM a hallucination risk?" | "It can't invent a reason, because it only receives findings the deterministic engine already produced, and there's a deterministic narrative underneath that's always sufficient on its own. The LLM adds prose; it never adds facts." |
| "What if the AI provider is down?" | "Nothing happens to decisions — it's off the request path entirely. The deterministic narrative renders and the demo continues." |

---

## Shared: what nobody should claim

| ❌ Never say | ✅ Say |
|---|---|
| "We detect 83% of fraud" | "0.83 PR-AUC on our own generated data — `[RECOVERED]`, pipeline validation, not a detection rate" |
| "Non-invertible tokens" | "Pseudonyms. The operator can't invert; a member could" |
| "This account was created 3 days ago" | "We *first saw* this account 3 days ago" |
| "Our accuracy is…" | "Accuracy is the wrong metric at this base rate" |
| "It's exact SHAP" | "Ablation-based attribution — real and per-transaction, not exact Shapley. It's labelled that way on screen" |

## Shared: what's honestly not built

Say this **before** anyone asks — it reads as judgment, not gaps.

- **Randomised control holdout** — needs live production traffic to mean anything
- **Full case management** — alerts exist; ring-level casework with SLA is workflow, not architecture
- **OPRF consortium crypto** — we ship HMAC + honest framing; upgrade is ~1 day behind the same interface
- **Exact TreeSHAP** — we use ablation, documented at `leaves_scorer.go:73`
- **Leaf-space novelty** — we use feature-space; documented at the top of `novelty/engine.go`

---

## Prep checklist

| Owner | Task |
|---|---|
| All | Read your section + the two Shared tables. **~40 min.** |
| All | Run `cd go && go test ./...` yourself once. See 29 pass. |
| D | Do a clean-laptop run from `RUN-ON-A-CLEAN-LAPTOP.md` end to end |
| B | Memorise the 6 numbers in your table |
| C | Practise the merchant lookup on the Graph screen until it's 5 seconds |
| D | Practise the Redis kill + recovery three times |
| All | One full timed run together, then one more |
