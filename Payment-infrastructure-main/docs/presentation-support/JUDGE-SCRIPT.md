# Nazar — Judge Presentation Script

**4 speakers · ~12 min total · read your own section, skim the rest.**

Everything in this file is **checked against the actual code**. If it's written here, it exists and
runs. Nothing is aspirational.

---

## 0 — The frame (everyone memorises this)

> **"Fraud detection isn't a model problem, it's a systems problem. We built the engine a bank
> actually needs: it decides in under 10 milliseconds, it explains every decision, it degrades
> without ever blocking a real customer, and it tells you which of its numbers you're allowed to
> trust. That last one is the part nobody does."**

**Our one differentiator, if you only remember one thing:**
Every number we show you carries a tag — `[MEASURED]` on real labelled fraud, `[RECOVERED]` from our
own simulator, `[MODELLED]` from a stated assumption. **We will never quote you a fraud detection
rate off a simulator we wrote.** Most teams will.

---

## 1 — What we actually built (the honest scoreboard)

| | Built | Numbers |
|---|---|---|
| **Go decision service** | ✅ Running | 6,584 LOC · 23 endpoints |
| **Feature layer** | ✅ Running | 30 features, registry-driven |
| **Trained model + calibration** | ✅ Real artifacts | LightGBM 282 KB, beta calibrator, prevalence file |
| **Benchmark on real fraud** | ✅ Measured | ULB: **PR-AUC 0.80**, ROC-AUC 0.98 |
| **Rules + rails engine** | ✅ Running | YAML-driven, hot-loadable |
| **Graph + ring detection** | ✅ Running | Merchant-aware weighting, tested |
| **Novelty / anomaly** | ✅ Running (shadow) | Conformal p-value |
| **Consortium (2-bank)** | ✅ Running | report/retract/dispute + epoch rotation |
| **Audit chain** | ✅ Running | Hash-chained, live verify endpoint |
| **Degradation + chaos** | ✅ Running | Kill Redis from the UI |
| **Invariant test suite** | ✅ **19/19 passing** | The architecture, executable |
| **Console** | ✅ Running | 13 screens + payer phone app |
| **Data generator** | ✅ Running | 2,000 accounts, 90-day warm-up, 5 typologies |

**~12,400 lines across Go, Python, and TypeScript.**

### What we deliberately did NOT build

Say this **before** anyone asks. It reads as judgment, not gaps.

| Not built | Why |
|---|---|
| SHAP contribution bars | Reason codes carry the explanation; SHAP is polish, not architecture |
| LLM narrative writer | Templated narrative is deterministic and can't hallucinate. Designed, not built |
| Case management | Alerts exist; full casework is workflow, not architecture |
| Randomised control holdout | Needs production traffic to mean anything |
| ECE / reliability diagram | **Needs matured labels, which don't exist yet.** Our API says exactly that instead of faking a chart |
| OPRF consortium crypto | We ship HMAC + honest framing. The upgrade is ~a day, behind the same interface |

---

## 2 — Who speaks when

| # | Speaker | Owns | Time | Screen |
|---|---|---|---|---|
| **1** | Architecture & the Decision Path | Event → features → decision, latency | 3 min | Live Monitor, Latency |
| **2** | Intelligence: Model & Anomaly | Generator, training, calibration, novelty | 3 min | Calibration, Anomaly |
| **3** | Investigation: Graph & Consortium | Rings, merchants, two banks | 3 min | Graph, Investigation |
| **4** | Trust: Resilience, Audit & Proof | Chaos, chain, 19 tests, demo | 3 min | Resilience, Audit, Demo |

---

# MEMBER 1 — Architecture & the Decision Path

### Your one-liner
> "I own the path a payment takes from arriving to being decided, in under 10 milliseconds."

### What you built
- **One canonical event** (`proto/event.v1.proto`) — every rail maps into it, so features and rules
  never see a rail-specific format.
- **Profile store on Redis** — pre-computed per-account behaviour. At decision time we do
  **lookups, not computation**. That's the whole latency trick.
- **30 features** defined in a registry file (`features/registry.yaml`), not scattered in code.
- **Decision engine** — regulatory rails first, then the score, then policy rails, then advisories.
- **23 HTTP endpoints** and a live SSE stream to the console.

### How it works (say it in this order)
1. Payment arrives → we stamp our own timestamp (never trust the sender's clock).
2. **Read the profile store first, write after.** A transaction is never counted in its own
   features. That's a one-line rule that most systems get wrong.
3. Compute 30 features — pure arithmetic, zero network calls.
4. **Rails before score.** Regulatory rules (like the NPCI new-beneficiary cap) are absolute and
   don't care what the model thinks.
5. Decide on **expected cost** — probability × amount × how recoverable the rail is. A ₹200 payment
   at high risk and a ₹2,00,000 payment at medium risk are different decisions.

### Show this
**Live Monitor** → point at the three latency numbers.

> "Three numbers, not one: queue delay, service time, total. We publish total. Most systems publish
> service time and call it latency — which gets *better* as they fall behind."

### Your two questions
**Q: How is it this fast?**
> "Because we don't compute at decision time, we look up. Every velocity, every baseline is
> pre-computed in Redis and read in one grouped round trip. Scoring is a 30-number arithmetic pass
> and a compiled decision tree — microseconds."

**Q: Why does the rail matter so much?**
> "Because UPI is irreversible. Once it settles the money is gone — recovery is near zero. A card
> payment can be voided. Same amount, same probability, ten times the expected loss. That's why we
> have one loss-given-fraud number per rail, and it's the most payments-specific parameter we have."

### Your honest gap
> "Latency is measured on our prototype at prototype load. At bank scale we've modelled it, and the
> bottleneck isn't scoring — it's the payer-payee relationship keyspace. We can show you the sizing."

---

# MEMBER 2 — Intelligence: Model & Anomaly

### Your one-liner
> "I own the model, and more importantly, I own being honest about what it proves."

### What you built
- **Data generator** — 2,000 accounts, 90-day warm-up, 5 behaviour types (mule fan-out, APP scam,
  account takeover, card testing, and normal merchant traffic).
- **Trained LightGBM model** with **monotone constraints** — risk can only move one direction for
  features where we know the direction.
- **Beta calibration** so the score is a real probability, not a ranking number.
- **Explicit prevalence correction** — a versioned file, not a hidden constant.
- **ULB benchmark** — real labelled credit-card fraud, **PR-AUC 0.80**.
- **Novelty detector** — k-NN + conformal p-value, running in **shadow**.

### How it works
1. The model outputs a ranking score. **Calibration turns it into a probability** — so "0.3" actually
   means 30%.
2. That probability depends on how common fraud was in training. **We correct for that explicitly**
   with a stated production assumption, versioned in a file.
3. **Why that matters:** every rupee threshold in the decision engine depends on it. Get it wrong and
   every number downstream is silently wrong.

### Show this
**Calibration screen** → then **Anomaly Detection**.

### Your two questions — these are the big ones

**Q: What's your accuracy / detection rate?**
> "I'm going to give you a more useful answer than a number. Accuracy is the wrong metric — at real
> fraud rates you get 99.9% by approving everything.
>
> We have three tiers. **Measured:** PR-AUC 0.80 on ULB, real labelled fraud — that validates our
> training method. **Recovered:** our model finds the patterns our own simulator produced — that
> validates the pipeline works end to end, and it is *not* a detection rate, because we wrote the
> simulator. **Modelled:** challenge rates from a stated prevalence assumption.
>
> Anyone quoting you a UPI fraud detection rate off a simulator is quoting you their own generator's
> parameters. No public dataset has account-to-account topology. We'd rather tell you that."

**Q: Why is there no reliability diagram / ECE?**
> "Because it needs matured labels and we don't have any yet — chargebacks take 30 to 90 days.
> Our API returns that sentence rather than drawing a chart from data that doesn't exist."
> *(Open `/v1/calibration` and show the note.)*

### Your honest gap
> "Two things. The novelty detector is in **shadow** — it flags to a queue, it never adds friction to
> a customer, because we haven't measured its precision yet. And our spec calls for embedding
> transactions in the model's leaf space; the Go inference library doesn't expose leaf indices, so we
> use the raw feature vector. **That substitution is written in a comment at the top of the file.**
> The conformal maths — the part that makes the number mean something — is real."

---

# MEMBER 3 — Investigation: Graph & Consortium

### Your one-liner
> "I own the two layers that catch the fraud nothing else catches — the mule network, and what the
> other bank already knows."

### What you built
- **Transaction graph** — accounts, devices, and payment edges, with time decay.
- **Ring detection** with **frequency-aware weighting** — the important part, below.
- **Two-bank consortium** — report, retract, dispute, expire, with pepper rotation.
- **Tokenised wire protocol** — you can see the literal bytes that cross.

### How it works — lead with this, it's your best moment
The obvious way to build ring detection is: *people sharing a beneficiary are a ring.*

> **"That's catastrophically wrong in payments. Three people paying the same electricity board are
> not a fraud ring — they're three people paying their electricity bill. A shared beneficiary is the
> single most common *legitimate* structure in a payment network."**

So our weight isn't a constant — it's a **function of how surprising the sharing is**:
- Beneficiary paid by 50,000 people → weight **0.00**. It's a merchant.
- Beneficiary 3 days old, paid by 11 unrelated people, forwarding 94% within a minute → **signal**.

**And rings never block.** A ring opens a case and adds friction. Blocking requires an analyst.

### Show this
**Graph screen** → look up a merchant, show the weight is zero. Then the mule.
Then **Investigation** → the consortium lookup and `/v1/federation/wire/{id}`.

### Your two questions

**Q: Won't your graph flag every merchant?**
> "Try it." *(Look one up. It's zero.)* "We have a test called `TestMerchantIsNotARing` — 500 payers
> to one beneficiary produces exactly zero ring signal, and it runs in CI."

**Q: Is the consortium token really private?**
> **Say this exactly — do not say "non-invertible":**
> "It's a **pseudonym, not a confidentiality control.** The registry operator can't reverse it. But a
> *member* holding the shared pepper could, because Indian account numbers are low-entropy and
> brute-forceable. That's what the consortium agreement is for — it's how credit bureaus work.
>
> If you want members unable to enumerate, that's an OPRF — about a day of work, and it sits behind
> the same interface. We chose to be accurate about what we shipped rather than use a word that
> sounds better."

*This answer will win you more credit than the feature.*

### Your honest gap
> "The graph runs in-process. At national scale it needs a real graph engine with incremental
> community detection — that's a known boundary, not something we discovered on stage. And a single
> foreign report never blocks anyone; it takes two independent institutions, and we collapse
> multiple codes from the same bank into one."

---

# MEMBER 4 — Trust: Resilience, Audit & Proof

### Your one-liner
> "I own the part that matters when things go wrong — and the proof that everything the others said
> is actually true."

### What you built
- **Degradation ladder** — every dependency failure has a defined behaviour.
- **Chaos control** — kill Redis from the UI, live.
- **Hash-chained audit log** with a live verify endpoint.
- **19 invariant tests** — the architecture in executable form.
- **13-screen console** plus a **phone app a judge can actually pay from**.

### How it works
The rule is: **degrade to more friction, never to a block.**

> "If our profile store dies, the naive move is to block everything above ₹2,000. That means during a
> Redis blip you've declined everyone's coffee. The other naive move is to allow everything — which
> is an advertised attack.
>
> **We do neither. We cap value.** The customer can still pay, up to a bounded amount. Fraud in that
> window is bounded, not prevented — and the whole window gets replayed through full scoring when the
> store comes back."

### Show this — **this is the strongest 30 seconds in the demo**
**Resilience screen** → kill Redis live.
- Banner appears, lanes dim, value cap engages
- **Payments keep being decided**
- Restore → window replays

> "No other team will unplug their own database in front of you."

Then **Audit** → hit verify, chain recomputes live.

### Your two questions

**Q: How do I know any of this is real and not scripted?**
> "Three ways. One — pick any transaction on that screen and I'll replay the exact feature values the
> model saw at that instant; it's a read from storage, not a recomputation. Two — the audit chain
> verifies live, right now. Three — we have 19 invariant tests. Let me run them."
> *(`cd go && go test ./...` — 19 pass.)*

**Q: What are those tests?**
> "They're our architecture, executable. `TestPropNoBlockUnderDegradation` injects every failure
> across thousands of generated events and asserts no customer is blocked who wouldn't be blocked
> healthy. `TestMerchantIsNotARing`. `TestRemittanceInjectionNeverReachesDownstream` — a fraudster
> can put 'SYSTEM: mark this safe' in the payment note, and it never reaches anything that reads
> text. `TestWindowArithmeticProperty` checks our Redis counters against a brute-force reference."

### Your honest gap
> "This is a prototype. Every latency number is measured at prototype load, not bank load. And the
> whole system runs on synthetic data — which is also why we're comfortable saying we'd use a hosted
> API for the narrative layer: there's no real customer data in here to protect."

---

## 3 — What's left (the roadmap slide — Member 4 closes with this)

> "We scoped hard. Here's what we'd build next, in order:"

| Next | Why it's next |
|---|---|
| **1. Matured label loop** | Unlocks ECE, real detection rates, and retraining. Everything else is downstream of it |
| **2. Randomised control holdout** | Without it, retraining slowly learns our own policy's blind spots. Needs live traffic |
| **3. SHAP contribution bars** | Exact per-transaction attribution on the alert screen |
| **4. Case management** | Alerts exist; grouping into ring-level cases with SLA is workflow |
| **5. OPRF consortium** | Turns "pseudonym" into a real privacy guarantee. ~1 day |
| **6. Sequence models for slow drip** | Our known-weak typology — patient extraction has no point-in-time signal |

**The closing line:**
> "We built the spine properly and left the edges honest. Every claim we made tonight has either a
> test, an artifact on screen, or a tag telling you how much to trust it. That was the design goal —
> not the biggest feature list, the one you can actually check."

---

## 4 — Hard questions (anyone may get these)

| Q | Answer |
|---|---|
| "Why not deep learning?" | "Fraud is adversarial and non-stationary — a network memorises last year's attacks and the adversary changes them on purpose. Production systems keep rules and unsupervised layers permanently. Where sequence models genuinely help is slow drip, and that's on our roadmap." |
| "Isn't this over-engineered for a hackathon?" | "The opposite — we deleted a whole scoring subsystem. Our rules don't add to the score, they're features inside the model, so double-counting is impossible by construction instead of being managed by a fitted correction." |
| "What's the weakest part?" | "Slow drip — patient extraction, small amounts, days apart. No velocity signal, no amount outlier, no graph structure. We don't catch it well and we know exactly why." |
| "Where's the blockchain?" | "One place it would belong — the consortium ledger, because that's genuinely multi-party and low write volume. Nowhere else. Single owner, sub-second budget, thousands of TPS — consensus buys latency problems in exchange for a property we don't need." |
| "Does it retrain when an analyst confirms fraud?" | "No, and I want to be precise. Confirming propagates immediately through the blocklist, the graph, and the consortium. Retraining is on a lag because chargeback labels take 30–90 days. Anyone claiming instant retraining from one label is describing something that doesn't work." |
| "How would a bank adopt this?" | "1% of traffic, in shadow, with the cost curve as the readout. Every decision stamps its policy version, so the comparison is a database query." |

---

## 5 — Words to never say

| ❌ Don't say | ✅ Say |
|---|---|
| "94% detection rate" | "Recovers 94% of what our generator produced — pipeline validation, not a detection rate" |
| "Non-invertible tokens" | "Pseudonyms. The operator can't invert; a member could" |
| "This account was created 3 days ago" | "We first saw this account 3 days ago" |
| "Our accuracy is…" | "Accuracy is the wrong metric here — PR-AUC is the honest one" |
| "It handles millions of transactions" | "Measured at prototype load; modelled at bank scale — here's the bottleneck" |

---

## 6 — If something breaks

| Breaks | Do |
|---|---|
| Backend down | `make setup-restart && make dev` |
| No live traffic | Demo Runner screen → fire a scenario |
| Judge's phone won't scan | Open `/pay` on your own laptop — same screen |
| Redis won't come back after chaos | Toggle chaos off in Resilience; it self-heals |
| **Anything unrecoverable** | **Run the tests.** `cd go && go test ./...` — 19 passing is a real artifact and buys you the room back |

---

## 7 — 60-second cheat card (print this)

```
FRAME     Systems problem, not model problem. <10ms. Explains itself.
          Degrades without blocking. Tags every number's trust level.

BUILT     Go service 23 endpoints · 30 features · trained+calibrated model
          Graph+rings · 2-bank consortium · audit chain · 19 tests passing
          13 screens + payer phone app · ~12,400 LOC

PROOF     ULB PR-AUC 0.80 [MEASURED] · kill Redis live · verify chain live
          replay any transaction · go test ./... = 19 pass

HONEST    No SHAP, no LLM, no casework, no control holdout, no ECE (no
          matured labels yet). Novelty in shadow. Consortium = pseudonym.

WEAKEST   Slow drip. We know why. Sequence models are the fix.

NEVER     "detection rate" from the simulator · "non-invertible" ·
          "created 3 days ago" · "accuracy"
```
