# Nazar — Real-Time Payments Fraud Detection

**Rev 2 — supersedes the prior plan. Timeline is not a constraint.**

Your team's research was mostly sound and wrong in three places that matter. One of the
corrections is not a fix — it's the pitch. This document audits the dataset work, rebuilds
the architecture around how payment fraud systems are actually constructed, and scripts a
demo in which the judge gets scammed on stage.

---

## Contents

- [00 — The reframe](#00--the-reframe-start-here)
- [01 — Dataset audit](#01--dataset-audit)
- [02 — Data strategy](#02--data-strategy)
- [03 — How real systems are built](#03--how-real-payment-fraud-systems-are-actually-built)
- [04 — What both plans got wrong](#04--what-both-previous-plans-got-wrong)
- [05 — Architecture](#05--architecture)
- [06 — Typology matrix](#06--typology-matrix)
- [07 — Scoring, done right](#07--scoring-done-right)
- [08 — Decision engine and friction](#08--decision-engine-and-the-friction-budget)
- [09 — Cases and investigation](#09--cases-and-investigation)
- [10 — Governance, consortium, blockchain](#10--governance-consortium-and-where-blockchain-belongs)
- [11 — Red team](#11--red-team)
- [12 — The demo](#12--the-demo)
- [13 — Q&A armor](#13--qa-armor)
- [14 — Build order](#14--build-order)
- [15 — What ports from Satyum](#15--what-ports-from-satyum)
- [Sources](#sources)

---

## 00 — The reframe, start here

Your team's doc contains this line: *"RBI mandates two-factor authentication for
transactions over ₹5,000."* That's wrong. Chasing down **why** it's wrong turned up the
single best framing available for this project.

On **25 September 2025** the RBI issued the **Authentication Mechanisms for Digital Payment
Transactions Directions, 2025**. They came into force on **1 April 2026**. Two things in
them matter:

- SMS OTP is being displaced as the default second factor, in favour of device-bound
  alternatives — biometrics, cryptographic keys, device fingerprinting, in-app confirmation.
- **Risk-based authentication is explicitly sanctioned.** Institutions may streamline
  low-risk flows and escalate beyond the 2FA minimum when a transaction looks anomalous.

> **Every regulated payment institution in India is, as of four months ago, required to own
> a risk engine that decides how much friction each individual transaction gets. That engine
> is the deliverable. Not a dashboard.**

This reframes the entire pitch. The problem statement's clause — *"minimizing customer
friction"* — stops being a nice-to-have you gesture at and becomes the regulatory centre of
the product.

**Opening line:**

> "RBI's Authentication Directions came into force on the first of April. They tell every
> bank in the country to stop sending an OTP for everything and start deciding, per
> transaction, how much friction is warranted. Nobody hands you the engine that makes that
> decision. We built it — and in the next four minutes one of you is going to get scammed
> by it."

### On the name

**Nazar** — the gaze, and simultaneously the ward against ill intent. It means watching and
protecting in the same word, which is exactly what the system does. Two syllables, unclaimed
in fintech, and it lets you say "Nazar flagged it" instead of "our system flagged it," which
sounds like a product rather than a project. Alternates: *Prahari* (sentry), *Pehra* (the
watch). Pick one in the first hour and use it in every file name, every slide, every sentence.

---

## 01 — Dataset audit

Each claim checked against primary sources. Summary: the factual numbers are almost all
correct, the *recommendation* built on them is unsafe, and the regulatory claim is wrong.

### ULB Credit Card Fraud (Option A) — VERIFIED

284,807 transactions, 492 frauds, 0.172%, European cardholders, two days in September 2013,
31 columns with V1–V28 PCA-anonymised. All correct. Your verdict — "poor for a real-time
dashboard, no identity fields" — is also correct.

**Keep it for exactly one purpose:** it is the most-cited fraud benchmark in existence, so a
PR-AUC on it is a number a judge can contextualise. One line on one slide. Nothing else. Do
not let it into your live path.

### IEEE-CIS (Option B) — VERIFIED, but the reason for rejecting it was incomplete

590,540 rows confirmed. "Single-digit % fraud" is 20,663 frauds = **3.5%** — use the real
number, vagueness looks like you didn't load it. Column count is 431 features (400 numeric,
31 categorical) plus ID and label; "434" is close but say 431. Vesta Corporation, time-split
train/test: correct.

You rejected it for being "too wide for 8 hours." That reason evaporates now that timeline
isn't a constraint — but the **real** disqualifier stands and is much more important:
*nothing in IEEE-CIS maps onto a live payment event.* It is card-not-present e-commerce with
anonymised, undocumented columns. A model trained on it is structurally incapable of scoring
a transaction from a phone in the room. If you report an IEEE-CIS AUC and a judge asks "is
that the model scoring my payment?", the honest answer is no. §02 turns that into an asset.

### Fraud Detection Handbook simulator (Option C) — facts verified, RECOMMENDATION UNSAFE

Every fact checks out. Scenario 1: any amount over 220 is fraud. Scenario 2: two terminals
drawn daily, all their transactions fraudulent for 28 days. Scenario 3: customers drawn
daily, a third of their transactions inflated and flagged over 14 days. 14,681 frauds, 0.8%.
Real datetimes, customer and terminal IDs, ~10k transactions/day.

**But you made it the primary dataset, and as-shipped that is a trap with two separate teeth.**

**Tooth one — the labels are a rule you can read.** Scenario 1 *is* a threshold. Train
XGBoost on this and it will learn `amount > 220` and post a spectacular AUC-PR. That number
measures nothing. Any judge who has opened this handbook — and it is the standard teaching
resource, so some have — will know that in one glance, and your headline metric becomes the
moment your credibility dies.

**Tooth two — wrong topology.** The simulator models *customer → terminal*: a card presented
at a merchant. Your entire fraud story is *account → account*: push payments, mule chains,
beneficiaries. **There is no payee node in this data.** No payee means no beneficiary
reputation, no fan-in, no mule graph, no consortium — four of your best features lost to a
schema mismatch nobody noticed.

**Verdict:** use the handbook as a reference implementation for feature engineering and
leakage-safe windowing — it is genuinely excellent for that — but do not use its data or its
labels.

### PaySim — you dismissed this too fast

"Fraud only occurs on TRANSFER and CASH_OUT" is correct. But you filed it under "less
feature-rich (no merchant identity)" and moved on, and that read the dataset backwards.

PaySim is mobile-money simulation with `nameOrig` and `nameDest` — **an origin account and a
destination account on every row.** It is the only public option in your list with a
payer→payee edge, which means it is the only one you can build a *graph* from. Its fraud
agent even runs the canonical pattern: drain an account by TRANSFER, then CASH_OUT through
the destination. That is a mule chain. Roughly 6.3M rows across 744 simulated hours.

Weaknesses are real — merchant nodes carry no balance, the fraud logic is simplistic, and
`isFlaggedFraud` is near-useless. Doesn't matter. You want the topology, and it's the only
one that has it.

### "RBI mandates 2FA for transactions over ₹5,000" — WRONG

Three different real rules compressed into one false one:

- **₹5,000 was the e-mandate ceiling** — from 1 Oct 2021, recurring auto-debits above ₹5,000
  needed AFA each time. Raised to **₹15,000** in June 2022, and to **₹1,00,000** for specific
  categories (insurance premia, mutual fund SIPs, credit card bills) in December 2023. It was
  never a general threshold for one-off payments.
- **₹5,000 is also the contactless-card tap ceiling** — no PIN below it. Different rule, same
  number, hence the confusion.
- **AFA on card-not-present is not amount-gated.** It applied broadly.

All of it is now superseded by the 2025 Authentication Directions in §00. Say the ₹5,000 line
to a room containing anyone from an Indian PSP or bank and you lose them. Say the Directions
line and you own the room.

### MuleHunter.AI — MISSING, add it

Your competitive research covered Stripe, Razorpay and Juspay well. It missed the one that
matters most for an Indian judging panel: **the regulator built a mule-detection system
itself.**

**MuleHunter.AI**, developed in-house by the Reserve Bank Innovation Hub, piloted from
December 2024. It studies nineteen distinct mule-account behaviour patterns and was built
explicitly as a replacement for the static rule-based systems banks were using. Live at
Canara Bank, PNB, Bank of India, Bank of Baroda and AU Small Finance Bank, with Federal Bank
in advanced stages; an RTI response in December 2025 put adoption at 23 banks. Canara
reported ~95% accuracy.

**Why this earns a slide:** the graph/mule layer is normally the part of a hackathon
architecture challenged as over-engineering. Here the central bank has already validated it,
in production, at named institutions. You are not proposing something speculative — you are
building the thing the RBI shipped, plus the real-time decisioning layer it doesn't have.

### Corrections summary

| Claim | Status | Correct version |
|---|---|---|
| ULB: 284,807 / 492 / 0.172% | OK | As stated |
| IEEE-CIS: "single-digit % fraud" | Vague | 20,663 / 590,540 = **3.5%** |
| IEEE-CIS: 434 feature columns | Close | 431 features (400 num, 31 cat) + ID + label |
| FDH scenarios & 0.8% rate | OK | As stated — but see the two teeth |
| FDH = best real-time fit | **No** | Wrong topology; no payee node |
| PaySim "less feature-rich" | **Backwards** | Only public set with a payer→payee edge |
| RBI 2FA over ₹5,000 | **Wrong** | Conflates e-mandate ceiling + contactless cap; superseded by Directions 2025 |
| PaySim fraud on TRANSFER/CASH_OUT | OK | As stated |

---

## 02 — Data strategy

The mistake in both plans was hunting for *one* dataset. There isn't one. Real institutions
run separate models per rail because the rails have different schemas and different
typologies — a card-not-present model and a UPI model are different models at the same bank.
Say that on stage and the "which dataset" question resolves itself.

| Source | Job | In the live path? | What you claim from it |
|---|---|---|---|
| IEEE-CIS | Credibility benchmark on real labelled fraud | No | PR-AUC + recall @ 1% FPR on a time-split holdout |
| ULB | Second benchmark, standard reference point | No | One PR-AUC. Proves the pipeline generalises |
| PaySim | Graph topology — mule chains, fan-out, cash-out | Feature validation | Graph features detect known mule chains in third-party data |
| **Your generator** | The live stream, the population, the attacks | **Yes — production** | Per-typology detection rates. Never a single headline accuracy |

**The line that defuses the whole dataset question:**

> "Our card-not-present model is trained on IEEE-CIS — 590,540 real labelled transactions,
> 3.5% fraud, time-split holdout. Our UPI model is a different model, because it's a
> different rail with a different schema and different typologies, exactly as it would be at
> a real bank. We publish both, versioned, in the registry — here." *(open the model registry)*

### Build your own generator — highest-leverage asset in the project

Timeline is not a constraint, so stop shopping and write it. Everything downstream — graph,
consortium, red team, demo, case management — depends on having a population you control.

**Population**

- **~2,000 accounts** with archetypes: salaried, student, small merchant, senior citizen, gig
  worker. Archetype sets amount distribution, hour-of-day histogram, payee-set size, device
  stability.
- **Payee graph**: each account has a stable payee set (family, landlord, 3–5 merchants) plus
  a long tail. This is what makes "new payee" mean something.
- **Devices**: mostly one per account. Shared devices exist — families, and mule farms.
- **Warm-up**: run 90 simulated days before the demo so every account has a real baseline. An
  account with no history is a cold-start problem, not a demo.

**Event schema — say ISO 20022**

Model your event on `pain.001` field naming (debtor, creditor, instructed amount, remittance
information, end-to-end ID). It costs nothing, it's the actual standard for payment messages,
and "our event schema follows ISO 20022 naming" is a sentence that ends a line of questioning.

```
TransactionEvent
  identity   end_to_end_id, timestamp_utc, rail{UPI|IMPS|CARD}
  parties    debtor_account, debtor_vpa, creditor_account,
             creditor_vpa, creditor_account_age_days
  amount     instructed_amount, currency
  channel    device_id, device_first_seen, app_version,
             ip, asn, geo_cell, session_id
  context    initiation{QR|INTENT|COLLECT|P2P}, remittance_info
  label      (arrives later — see label latency, §10)
```

**Fraud typologies to generate**

The handbook's Scenario 1 is a threshold. Yours must not be. Each typology is generated by an
*agent with a strategy*, not a rule, and each has an evasion knob you can turn during the demo.

| Typology | Generator behaviour | Evasion knob |
|---|---|---|
| **APP scam** (authorised push payment) | Victim socially engineered; pays a 3-day-old beneficiary a large round amount, single transaction, own device, correct PIN | Amount just under the victim's own p95 |
| **Mule fan-out** | Beneficiary receives from 8–20 unrelated payers within an hour, forwards 90%+ onward within 60s, 2–3 hops to a cash-out node | Forwarding delay; hop count |
| **Account takeover** | New device + new IP/ASN, then payee-set change, then escalating amounts over 20 minutes | Dwell time before first payment |
| **Card testing** | Burst of small authorisations across many BINs from one device/ASN | Inter-arrival jitter |
| **Smurfing** | One logical transfer split into N sub-threshold payments across M beneficiaries | N, and spread across time |
| **Slow drip** *(your honest failure)* | Patient extraction: small amounts, days apart, each individually unremarkable | Interval — turn it up until you miss it |
| **Synthetic identity** | New account builds 6 weeks of clean history, then bursts | Ramp length |

> **Label honesty.** Because you write the generator, you own the labels — so never quote a
> single headline accuracy from it. Quote **per-typology detection rate** instead, and include
> the one you fail. "94% on mule fan-out, 91% on ATO, 43% on slow drip, and here's why slow
> drip is hard" is far stronger than "97% accurate," because the first is a claim a fraud
> practitioner recognises and the second is one they've learned to distrust.

---

## 03 — How real payment fraud systems are actually built

Your §2 was a good start — it correctly identified rules-on-top-of-ML, behavioural
baselining, step-up auth, and network signal sharing. Here is the structural picture
underneath, drawn from what Stripe Radar, Feedzai, Featurespace ARIC, NICE Actimize, Visa
Advanced Authorization and Mastercard Decision Intelligence publicly describe. Six things
recur; five were missing from both plans.

**1. The profile store is the system.** Not the model. Every serious platform is organised
around a two-tier feature store: streaming aggregates in memory (velocity counters over
sliding windows) and batch behavioural profiles (30/90-day baselines). Featurespace's entire
product thesis is "adaptive behavioural analytics" — a per-entity model of normal that updates
continuously. Stripe's documented advice is to use persistent Customer objects specifically so
patterns accumulate.

The consequence: **every threshold is a deviation from that entity's own baseline, never an
absolute.** ₹50,000 is a Tuesday for one account and a six-sigma event for another. This one
design choice separates a system from a demo, and it takes one sentence on stage.

**2. Decisions are not binary, and they're not made on the score.** Allow / step-up / hold /
block, driven by **expected loss** — P(fraud) × amount — not the score alone. This is why a
₹200 payment at score 80 sails through and a ₹2,00,000 payment at score 55 gets challenged.
Score-band tables like 0–30 / 31–60 / 61–80 / 81–100 ignore amount entirely, which is the
single most important variable in the loss function.

**3. Reason codes are infrastructure, not a feature.** Visa Advanced Authorization emits
reason codes alongside its score because the issuer downstream has to act on them.
Explanations aren't a UI nicety — they're the interface between the model and every human and
system downstream, and increasingly a legal requirement for automated decisions.

**4. Labels arrive late, and the system is designed around that.** Analyst dispositions come
back in minutes to hours. Chargebacks come back in **30 to 90 days**. Stripe's public
description of Radar notes ground truth arrives automatically from disputes flowing through
the network — which is to say, weeks later. Every real platform has a label store handling
this asymmetry and retrains on a lag. No hackathon models it. Modelling it costs you a counter
and a colour, and it is one of the strongest seniority signals available.

**5. New models run in shadow before they decide anything.** Champion–challenger. The new
version scores every transaction and decides nothing while you compare against the incumbent.
Cheap, and it shows you understand that shipping a model is a deployment problem, not a
training problem.

**6. The latency budget is a hard constraint that shapes the design.** An authorisation
decision has roughly 100–300ms end to end, of which fraud scoring gets a slice. That's why
real systems precompute aggressively: at decision time you do *lookups*, not computation. It's
also why p99 matters and p50 is close to meaningless — the tail is where timeouts and
fail-open live.

**Names to add to your competitive research:** *Feedzai* and *Featurespace ARIC* (behavioural
analytics — ARIC's adaptive per-entity modelling is the direct precedent for your baseline
layer). *NICE Actimize* (case management, which is half your problem statement and 5% of most
builds). *Visa Advanced Authorization* / *Mastercard Decision Intelligence* (network-level
scoring with reason codes). *Confirmation of Payee* (UK) and the UK PSR's mandatory APP
reimbursement regime from October 2024 — the direct precedent for the payee-warning
interstitial in your demo, and proof that "warn the payer about the beneficiary" is a real
regulated control rather than something you invented.

---

## 04 — What both previous plans got wrong

Including mine.

### The earlier architecture I gave you

- **Too wide, too thin.** Rules + LightGBM + Isolation Forest + graph + consortium +
  blockchain + MCP agent + adversarial suite, all at equal weight. Every layer shallow. Judges
  read wide-and-thin as unfinished.
- **The IEEE-CIS hole.** I proposed training on IEEE-CIS *and* scoring live phone payments
  without noticing those are incompatible schemas. One question destroys it.
- **"Generalises to networks it has never seen"** is a research claim you cannot defend, and
  it invites exactly the scrutiny that kills you.
- **Typing cadence on a UPI flow.** A UPI payment is an amount and a PIN. There is no keystroke
  dynamics signal in four digits. It would have felt mocked, because it would have been.

### Your team's Rev 1

- **The scoring formula is incoherent.** `0.55·ml + 0.25·anomaly + 0.20·rules` takes a
  calibrated probability, adds an unnormalised Isolation Forest score and a capped rule sum,
  and produces a number that is no longer a probability. You can't compute expected loss from
  it, can't set thresholds economically, can't defend the constants. "Why 0.55?" has no answer.
- **Double counting.** Your rules use amount-ratio and velocity. Your model uses amount-ratio
  and velocity. Adding them counts the same evidence twice, with hand-set weights.
- **Isolation Forest at 25% is far too heavy** for a signal with that precision profile.
- **Score bands ignore amount** — see §03 point 2.
- **Half the problem statement is a "SHOULD."** Alert management and investigation workflows
  are two of the five named capabilities and sit at hours 6.5–7.5 flagged as optional.
- **No payee, no graph, no mule story** — a direct consequence of the FDH topology mismatch.
- **The 8-hour frame drove the architecture.** Timeline is not a constraint. Rebuild from
  what's right.

### What Rev 1 got right — keep verbatim

- Backward-only windowing for rolling aggregates, called out explicitly as leakage prevention.
- Chronological train/test split, never random shuffle.
- `scale_pos_weight` over SMOTE.
- PR-AUC and recall-at-fixed-FPR over accuracy and ROC-AUC.
- Never feed raw high-cardinality IDs to the tree; aggregate them into behavioural features.
- **LLM strictly downstream of deterministic scoring, as a phrasing layer only.** Exactly
  right — keep that boundary and state it explicitly on stage.
- Python end-to-end so the model and the API share a process.

---

## 05 — Architecture

```
  PAYMENT SURFACE              REPLAY / RED TEAM
  phone PWA, QR, PIN           population replay · attack injector
        │                              │
        └──────────────┬───────────────┘
                       ▼
              [L0] INGEST  ISO 20022-shaped event · Redis Stream
                       ▼
              [L1] PROFILE STORE   ← the system
              streaming aggregates (1m/5m/1h/24h/7d/30d)
              behavioural baselines (median, MAD, hour hist, payee set)
              graph metrics (fan-in, fan-out, device degree, hop depth)
              keys: payer · payee · device · ASN · (payer,payee)
                       ▼
              [L2] SCORING   all four read L1, none compute from raw
              ├ policy rules (YAML, hot-reload, reason-coded)
              ├ supervised GBM → calibrated P(fraud)
              ├ novelty (IsolationForest + robust z) → unknown-pattern queue
              └ graph risk (beneficiary reputation, ring membership)
                       ▼
              [L3] DECISION ENGINE
              expected loss = P × amount · hard rails · friction budget
              → ALLOW │ STEP-UP │ HOLD │ BLOCK  + reason codes + latency
                       ▼
        ┌──────────────┼──────────────┬──────────────┐
        ▼              ▼              ▼              ▼
  [L4] CASES   [L5] WORKBENCH  [L6] GOVERNANCE  [L7] CONSORTIUM
  grouped by    entity 360     registry·shadow   hashed identifiers
  entity/ring   link graph     drift·audit       signed append-only
  expected-loss money flow     label store       Bloom filter → Bank B
  priority      time machine
        │                                               │
        └──────── analyst disposition ──────────────────┘
                  → block list · entity risk · graph propagation · publish
```

### Stack

| Layer | Choice | Why this and not the obvious alternative |
|---|---|---|
| Ingest | Redis Streams | Kafka's semantics without Kafka's operational tax. Say "Kafka-compatible consumer-group semantics" — it's true and it's the honest version |
| Profile store | Redis (hashes + sorted sets) | Sorted sets give sliding-window counters natively. O(1) reads at decision time is the whole point of the latency budget |
| Scoring API | FastAPI, model in-process | Your call, and it's right. No serialisation bridge |
| Models | LightGBM + isotonic calibration | LightGBM over XGBoost for categorical handling and speed. Calibration is non-negotiable — see §07 |
| Graph | NetworkX in-process, Postgres edge table | Neo4j is a day of setup for a graph of a few thousand nodes. Don't |
| System of record | Postgres | Transactions, decisions, cases, labels, audit chain |
| Push to UI | **Native WebSocket from FastAPI** | **Diverging from Rev 1.** Supabase Realtime couples your fraud latency to a DB replication hop, and your headline metric is decision latency. Own the socket, own the number. ~30 lines in FastAPI |
| Console | React + TS + Tailwind, custom charts | Recharts for standard charts. Link graph and money-flow need custom SVG/Canvas |

> **The one design rule that ties it together.** Nothing in L2 computes anything from raw
> history. Every feature is a lookup against L1, computed as the event arrived. That is what
> makes p99 defensible, and it is *also* what makes the Time Machine in §09 possible — if
> features are materialised at decision time, you store them with the decision and replay
> exactly what the model saw. Point-in-time correctness stops being a discipline you promise
> and becomes a property you demonstrate.

---

## 06 — Typology matrix

Worth a slide on its own. It proves you thought about fraud rather than about classifiers, and
it pre-empts "why do you need all these layers" by showing no single layer covers the space.

| Typology | Rules | Supervised | Novelty | Graph | Consortium | Detect |
|---|---|---|---|---|---|---|
| **APP scam** | weak | weak | medium | **strong** | **strong** | — |
| **Mule fan-out** | medium | medium | weak | **strong** | medium | — |
| **Account takeover** | **strong** | **strong** | medium | weak | weak | — |
| **Card testing** | **strong** | medium | medium | medium | weak | — |
| **Smurfing** | weak | weak | medium | **strong** | weak | — |
| **Slow drip** | weak | weak | weak | weak | weak | — |
| **Synthetic identity** | weak | medium | weak | medium | medium | — |

Fill the Detect column from your own red-team runs (§11). Fill it honestly.

### Why APP scam is the centre of your pitch

Authorised push payment fraud is the dominant fraud in Indian digital payments and it defeats
the entire authentication stack by construction. The victim was not hacked. They logged in
from their own phone, on their own network, entered their own correct PIN, and authorised the
payment themselves. Every device signal is clean. Every behavioural-authentication signal is
clean. **Sending an OTP does not help — the victim will enter it, because they believe they
are supposed to.**

Look at the matrix row: rules weak, supervised weak, novelty medium. The only strong columns
are graph and consortium. That is *why* those layers exist — not because graphs are
impressive, but because there is a dominant fraud class nothing else touches. The two signals
that work:

- **Beneficiary reputation** — the receiving account is 3 days old, has received from 11
  unrelated payers in the last hour, and forwards everything onward within a minute. Nothing
  about the *payer* is suspicious. Everything about the *payee* is.
- **Cross-institution knowledge** — that beneficiary is new to your bank and already confirmed
  at another. Which is precisely why the consortium layer is load-bearing, not decorative.

> Build the demo around APP fraud and you are demonstrating the hardest and most current
> problem in Indian payments — and every layer of your architecture becomes necessary rather
> than impressive.

---

## 07 — Scoring, done right

### Step 1 — calibrate, so the score is a probability

A GBM's raw output is a ranking score, not a probability. Fit **isotonic regression** on a
held-out slice so that among transactions scored 0.30, roughly 30% are actually fraud. Ten
lines of scikit-learn.

Three things follow that you cannot have without it: expected loss becomes computable;
thresholds become economic rather than arbitrary; and you get a **reliability diagram** —
predicted probability against observed frequency — a chart essentially no hackathon team
produces and which any judge with an ML background will immediately recognise as the mark of
someone who has shipped a model.

### Step 2 — combine in log-odds, with learned weights

```python
# Rev 1 — do not ship this
score = 0.55*ml + 0.25*anomaly + 0.20*rules
#  ✗ output is not a probability     ✗ constants indefensible
#  ✗ double-counts shared features   ✗ breaks expected loss

# Rev 2
logit_final = logit(p_ml) + Σ wᵢ · 1[ruleᵢ fired]
p_final     = sigmoid(logit_final)

# the wᵢ are FIT, not chosen: a logistic regression of the label on
#   [logit(p_ml), rule_1, ..., rule_n] over a held-out slice,
#   with logit(p_ml) OFFSET-CONSTRAINED to coefficient 1.0 so each
#   wᵢ is the *incremental* evidence beyond the model.
#
# graph metrics are features inside p_ml — no separate graph_risk term.
# novelty and consortium are ADVISORY, not score contributions (Step 3, §15C).
```

> Earlier drafts had `+ w_g·graph_risk + w_n·novelty_z` in this sum. That contradicted Step 3
> below and stacked a hand-fitted sub-model on top of a fitted model. See
> [ARCHITECTURE.md §17](ARCHITECTURE.md#17--corrections-to-the-playbook).

What this buys you, as answers to questions you will be asked:

- *"Why those weights?"* — "We didn't pick them, we fit them. Here's the coefficient table
  with confidence intervals."
- *"Aren't your rules and your model using the same features?"* — "Yes, and the fit handles it.
  The model's own log-odds is offset-constrained to coefficient 1.0, so each rule weight is
  purely the incremental evidence beyond the model. A rule the model already captures fits to
  zero by construction. Three of our fifteen did; we kept them as interpretable overrides."
- *"Is the output still meaningful?"* — "It's still a calibrated probability, which is what the
  decision layer needs."

### Step 3 — give Isolation Forest a job it's actually good at

At 25% of a blended score, Isolation Forest mostly adds noise and re-flags what the GBM already
caught. Its real value is orthogonal: **finding transactions that don't resemble anything in
training.**

Route on the *disagreement*, not the sum. High novelty + low supervised score is the
interesting quadrant — "this is unlike anything we've seen, and the model has no opinion
because it has no precedent." Those go to a dedicated **Unknown Pattern** queue.

> **Demo beat.** Invent a brand-new typology on the spot during the red-team phase — one never
> generated during training. The supervised model shrugs. The novelty detector fires and drops
> it in the Unknown Pattern queue. *"That's a zero-day. The model has never seen it and
> correctly has no opinion. The system still caught it, and it caught it by noticing it had
> never seen it before."* That single beat is a better argument for unsupervised learning than
> any slide, and it answers the problem statement's "anomaly detection" clause with something
> other than a synonym.

### Step 4 — the rule layer as a hot-reloadable artefact

```yaml
- id: R-014
  name: beneficiary_fanin_burst
  typology: mule_fanout
  reason_code: BEN_FANIN_1H
  when: payee.distinct_payers_1h >= 8
        and payee.account_age_days < 30
  severity: high
  explain: "{{payee.distinct_payers_1h}} unrelated payers sent
            to this account in the last hour; account is
            {{payee.account_age_days}} days old."
```

Hot-reload exists for one reason: when a judge says "what if I want to catch X," you open the
file, type a rule, save it, and it fires on the next transaction while they watch. Live
authoring beats any claim of extensibility.

---

## 08 — Decision engine and the friction budget

This is the layer that makes it a payments product rather than a classifier with a UI, and it
is the layer the RBI Directions in §00 are about.

### Decide on expected loss

```
expected_loss = p_final × instructed_amount

# hard rails — bypass the score entirely, always
if payee in consortium_blocklist          → BLOCK
if payee in confirmed_mule_ring           → BLOCK
if payer.velocity_1h > rail_cap           → BLOCK
if payee.first_seen < 24h and amt > 5000  → CAP   (NPCI cooling period)

# graduated response on expected loss
EL < ₹50            → ALLOW            no friction, invisible
₹50  ≤ EL < ₹500    → ALLOW + monitor
₹500 ≤ EL < ₹5,000  → STEP-UP          device-bound factor
EL ≥ ₹5,000         → STEP-UP + payee warning interstitial
                       then HOLD or BLOCK on override
```

The **NPCI new-payee cooling period** rail is worth naming explicitly on stage: for the first
24 hours after adding a beneficiary, UPI transfers to them are capped at ₹5,000. It is a real
production control, it is a rail rather than a model, and citing it shows you know payment
systems already have non-ML defences you're layering on top of rather than replacing.

### Friction as a first-class, measured quantity

The problem statement says "minimizing customer friction." Almost every team will treat that
as a sentence to nod at. Make it a number on the wall.

| Metric | Definition | Why it's on the wall |
|---|---|---|
| `challenge_rate` | % of legitimate transactions given a step-up | This is the friction. Every point costs conversion |
| `value_recall` | % of fraud **value** stopped | Not transaction count. Stopping 90% of frauds and 20% of the money is failure |
| `false_block_rate` | % of legitimate transactions hard-blocked | The one that generates complaints and churn |
| `step_up_pass_rate` | % of challenges the user completes | Distinguishes friction from a wall |
| `p99_decision_ms` | ingest → decision, 99th percentile | The tail is where fail-open lives. p50 is meaningless |

### The cost curve — your closing image

```
total_cost(θ) = fraud_value_missed(θ)
              + challenge_rate(θ) × abandonment_rate × avg_margin
              + false_blocks(θ) × cost_per_complaint
```

It's a U. Your operating point sits at the minimum and you can say exactly why. Put the
threshold on a slider in the console and drag it while the curve tracks and the metrics move
live.

> "At our operating point we stop **94% of fraud by value** while challenging **1.8% of good
> customers**. This dial isn't ours to set — it's the bank's risk appetite. So we made it a
> dial." *(drag it)* "Here's a conservative institution. Here's an aggressive one. Same engine,
> same code, different policy version — and that version is stamped on every decision in the
> audit log."

---

## 09 — Cases and investigation

Two of the five named capabilities are *alert management* and *investigation workflows*. Rev 1
had them as "SHOULD" items at hour 6.5. Most teams ship a table with a status dropdown.
Building this properly is the cheapest available differentiation, because it's the part nobody
bothers with.

### Cases, not rows

An analyst does not work transactions. They work **cases**. A mule ring with 40 transactions
across 14 accounts is *one* case, not 40 alerts. Group alerts by entity and by connected
component in the graph, and prioritise the queue by **total expected loss** with an SLA
countdown — that's the ordering a real fraud ops floor uses.

Your queue shows six cases, one representing ₹4.2 lakh across a fourteen-account ring. The
other team's queue shows two hundred rows sorted by time. That comparison is the entire
argument.

### The alert detail screen

The screen judges look at longest, so it's where the design budget goes.

- **Reason codes with fitted contributions** — horizontal bars, signed, from the log-odds
  decomposition in §07. Not feature importances. Per-transaction contributions.
- **Baseline vs. this transaction** — the payer's own 30-day amount distribution with this one
  marked. The most legible chart in the product.
- **Beneficiary card** — age, fan-in over 1h/24h, forwarding latency, consortium status. For
  APP fraud this panel *is* the case.
- **Counterfactual** — "this would have been allowed if the beneficiary had ≥3 prior
  transactions with this payer." Perturb one feature, rescore, report the flip. Far more useful
  to an analyst than a SHAP bar, and cheap for a rules+GBM stack.
- **Auto-drafted narrative** — plain-English case summary generated from the structured reasons
  only, LLM strictly as a phrasing layer downstream of deterministic scoring.

> *"The analyst didn't have to reconstruct any of this. It was written before they clicked."*

### Time Machine

Scrub to any past decision and see the exact feature values the model saw at that instant — not
recomputed from today's data. Because L1 materialises features at decision time and stores them
with the decision (§05), this is a read, not a reconstruction.

What it demonstrates: point-in-time correctness and the absence of training/serving skew, the
failure mode that quietly ruins most production fraud models. It's also the unanswerable
response to "how do I know this isn't scripted" — you replay an arbitrary transaction the judge
picks.

### Disposition has visible consequences

When an analyst hits Confirm Fraud, four things happen and the UI shows all four:

1. Beneficiary → block list, effective on the next transaction
2. Entity risk scores updated for the account and its device
3. Risk propagated one hop through the graph to connected accounts
4. Hashed identifier published to the consortium ledger

Say precisely what this is: "the system learns" is a claim about list and graph propagation,
which is honest and immediate. It is **not** retraining, which happens on a lag against the
label store (§10). Making that distinction out loud, unprompted, is worth more than the feature.

### Investigation agent

An LLM over your own graph tools — "find every account sharing this device fingerprint," "trace
two hops downstream from this beneficiary," "which other cases touch this ASN." It queries tools
you wrote and returns structured results the console renders. It never scores, never decides,
never invents a reason.

---

## 10 — Governance, consortium, and where blockchain belongs

### Governance — cheap to build, disproportionate credibility

- **Model registry** — every model with version, training dataset, date, metrics, feature list.
  Two entries: `cnp-v1` (IEEE-CIS) and `upi-v1` (yours). This screen is where §02's dataset
  answer becomes visible rather than verbal.
- **Shadow mode** — `upi-v2` scores everything, decides nothing. Agreement matrix against v1.
- **Drift monitor** — PSI on the top features, one chart, one threshold line.
- **Label store with honest latency** — analyst labels land in minutes, chargebacks on a 45-day
  lag. A "labels pending" counter with a maturity distribution. Nobody does this. It's a counter.
- **Fairness note** — list the attributes deliberately excluded from the feature set.
- **Hash-chained decision log** — every decision hashed with its predecessor. Tamper-evident.

### Consortium

Two bank instances, side by side. A confirmed mule beneficiary at Bank A publishes a hashed
identifier to a signed append-only log, distributed as a Bloom filter. Bank B blocks a payee it
has never seen, seconds later. Show the wire payload: a hash and a signature. No customer data
crosses.

Reporter reputation weighting handles the failure mode that kills real consortia: an institution
that repeatedly flags accounts which turn out clean has its weight decay, provably, because
every entry is signed.

This exists because of §06. APP-scam beneficiaries are new to your bank and already known at
another. Cross-institution knowledge isn't a bonus layer — it's the only thing that catches the
dominant typology early.

### Slide: where blockchain belongs, and where it doesn't

**Belongs**

- **Consortium intelligence.** Multiple institutions who don't fully trust each other, needing a
  shared append-only record with no single owner and provable attribution for bad reports. Low
  write volume.
- **Audit trail.** Non-repudiable decision log. Be honest that a hash chain in Postgres gets
  ~90% of this — you gain tamper-*resistance*, which only matters if you don't trust your own DB
  operator.

**Doesn't**

- The transaction ledger. The scoring path. Smart contracts making fraud decisions. Any token.
- Single owner, sub-second latency budget, thousands of TPS. Consensus buys you latency and
  throughput problems in exchange for a property you don't need. A judge who knows infrastructure
  spots this instantly on every other team that bolted it on.

Deliver the slide as two columns with the *reasons*, not the conclusions. Restraint reads as
seniority and is far harder to attack in Q&A than enthusiasm. Build the Bloom filter first — it
works standalone and demos in thirty seconds. Add the signed ledger underneath once it's solid.

---

## 11 — Red team

An attack console, operated by a second person on your team, live, while you narrate. Each
typology from §02 is a button with parameter sliders. Fire them individually or all at once.
Output is the §06 matrix filling in live: detection rate, median time-to-detect, value-stopped.

> **The most important paragraph in this document.**
>
> **Show the failure.** Turn the slow-drip interval up until detection collapses. Put it on
> screen: *slow drip, 43%.* Then say why — patient extraction produces no velocity signal, no
> amount outlier, no new-payee event, and no graph structure, because each transaction is
> genuinely unremarkable in isolation. Then say what fixes it: sequence models over the account's
> transaction history rather than point-in-time features, which is a real research direction and
> a real roadmap item.
>
> Teams that claim 99% get probed until something breaks in front of the judges. A team that
> walks up to its own weakest point and explains it has removed the entire attack surface of Q&A,
> and has demonstrated the one thing genuinely hard to fake: knowing what your system can't do.

Theatrically, two operators — one attacking, one defending — is a structure no other team will
have. It makes the adversarial nature of fraud legible in a way no slide can, and it turns your
metrics from claims into events the judges watched happen.

---

## 12 — The demo

Three surfaces, visible simultaneously: **the victim's phone** (mirrored into a corner of the
projected view), **the operations console** (main), **Bank B and the consortium** (secondary
screen or a split). The judges watch one event from three vantage points at once. That
composition alone communicates "system" before you say a word.

### Act 0 — 0:00–0:15 — It was already running

The room walks in to a live console. Traffic flowing, latency graph steady, map lit, everything
green. Do not open a laptop. Do not load anything.

> "This has been running since we set up. Nothing you're about to see is a recording."

### Act 1 — 0:15–1:15 — The judge gets scammed

Hand a judge a phone. First, a normal payment — scan a QR, coffee, ₹120. Green. **38ms.** No
friction, no OTP, nothing. Point at the number.

Then hand them a second phone showing a message: *"Sir, your KYC has expired. Verify immediately
by transferring ₹49,999 to the account below."* With a QR. Ask them to scan it.

The payment does **not** get blocked. That would be arrogant and wrong. It gets an interstitial:

```
Before you send ₹49,999

This account was created 3 days ago.
11 people have paid it today. It forwards money on within a minute.
Another bank reported it as a mule account 4 minutes ago.
You have never paid this account before.
```

Let them tap "I'm sure." Expected loss clears the hard rail — it blocks anyway, with the
beneficiary's consortium status as the reason. Two-stage friction: warn, then refuse.

> "That's authorised push payment fraud. They weren't hacked. Own phone, own network, correct
> PIN. An OTP wouldn't have helped — they'd have typed it, because they thought they were
> supposed to. Nothing about the payer was wrong. Everything about the payee was."

### Act 2 — 1:15–2:15 — The console

Turn to the main screen. The case is already open — you didn't navigate to it, it was raised
while the judge was reading the warning.

Reason codes with signed contribution bars. The judge's own 30-day baseline with this
transaction sitting six sigma out. The beneficiary card: 3 days old, fan-in 11 in the last hour,
forwarding latency 41 seconds. The counterfactual line. The auto-drafted narrative.

> "The analyst didn't reconstruct any of this. It was written before they clicked."

### Act 3 — 2:15–3:00 — Pull the thread

Click the beneficiary. The graph opens: 14 accounts, 3 shared devices, converging on one cash-out
node. Then the money-flow view — victims on the left, layering in the middle, exit on the right.
Your judge's ₹49,999 is one strand among many.

> "Your money was going here."

Analyst hits Confirm Fraud on the ring. Four things animate at once: block list, entity risk,
one-hop graph propagation, consortium publish. Say what each one is, and say explicitly that this
is propagation, not retraining.

### Act 4 — 3:00–3:30 — The other bank

Second screen: a different institution. Within seconds, one of Bank B's customers attempts a
payment to the same mule account. Blocked, on first contact, an account Bank B has never seen.

Show the wire payload — a hash and a signature.

> "No customer data crossed. That's the point. This is the layer where a distributed ledger earns
> its place, and it's the only layer where it does."

### Act 5 — 3:30–4:15 — Now attack it properly

Second operator opens the red-team console. Seven typologies, two thousand transactions, fired at
once. The matrix fills in live.

Then the invented typology — one never in training. Supervised model shrugs; novelty detector
fires; it lands in the Unknown Pattern queue.

> "That's a zero-day. The model has no opinion because it has no precedent. We caught it by
> noticing we'd never seen it before."

Then land on the failure. *Slow drip: 43%.* Explain why. Explain the fix. **Do not skip this.**

### Act 6 — 4:15–4:45 — The dial

The cost curve with your operating point marked. Drag the threshold; the curve, the challenge
rate, the value-recall and the rupee figures all move together.

> "94% of fraud by value, 1.8% of good customers challenged. This dial belongs to the bank's risk
> appetite, not to us — so we made it a dial. Conservative. Aggressive. Same engine, different
> policy version, and that version is stamped on every decision in the audit log."

### Act 7 — 4:45–5:00 — Close on governance

Model registry with two models and their datasets. Shadow model agreement matrix. Drift chart.
Labels-pending counter with the 45-day maturity curve. Hash-chained audit log. The blockchain
scoping slide.

> "RBI's Authentication Directions came into force on the first of April. This is the engine they
> assume every bank now has."

### Production craft

| Item | Rule |
|---|---|
| Navigation | Zero menu clicks on stage. Every act is a keyboard shortcut. Rehearse until your hands know them |
| Loading | No spinner is ever seen. Pre-warm everything. 90 days of history seeded before the room opens |
| Sound | One alert tone, used exactly twice — the APP block and the Bank B block. Any more and it's noise |
| Numbers | `font-variant-numeric: tabular-nums`, fixed-width slots. A metric that jitters looks broken even when it's right |
| Projector | Rehearse on the actual projector. Projectors crush dark greys — a dark theme tuned on your laptop becomes a black rectangle on stage |
| Narrator strip | A running plain-English commentary line on the console. It's the auto-drafted narrative surfaced live, and it's how the non-technical judge follows along |
| Safe mode | A recorded session that replays through the **real** UI. If the network dies you keep going and nobody can tell. Plus a screen recording as last resort |
| Rehearsal | Five full runs minimum, stopwatch on every act. Act 1 must be flawless — practise handing the phone over, that's the beat that goes wrong |
| Visual system | An operations console, not a consumer dashboard. Dense, calm, monospaced numerals, one accent, semantic colour reserved strictly for risk state. Model it on a trading desk or a SOC |

---

## 13 — Q&A armor

**Q: What dataset did you use?**
"Three, for three different jobs. The card-not-present model trains on IEEE-CIS — 590,540 real
labelled transactions, 3.5% fraud, time-split holdout. Graph features are validated against
PaySim, which has real payer-to-payee edges. And the live UPI stream comes from a population
simulator we wrote, because no public dataset has the account-to-account topology this rail
needs. They're all in the model registry with their metrics." *(open it)*

**Q: What's your accuracy?**
"PR-AUC on the IEEE-CIS holdout, and recall at 1% false-positive rate — those are the right
metrics at 3.5% base rate; accuracy and ROC-AUC both look artificially good on imbalanced fraud
data."
"But for the live system I'd rather give you the honest version, which is per-typology: 94% on
mule fan-out, 91% on account takeover, 43% on slow drip. That last one we don't catch well, and
I can tell you exactly why."

**Q: Is that the model scoring my payment?**
"No — and that's deliberate. IEEE-CIS is card-not-present e-commerce; your payment is a UPI push
transfer. Different schema, different typologies, so it's a different model, exactly as it would
be at a real bank running multiple rails. Both are in the registry, versioned, with their own
training data and metrics."

**Q: Is this real or mocked?**
"You made that payment ninety seconds ago on your own phone. Pick any transaction on that screen
and I'll replay the exact feature values the model saw when it decided." *(open Time Machine on
whatever they point at)*

**Q: Why not deep learning?**
"Fraud is adversarial and non-stationary — a network memorises last year's attack patterns, and
the adversary changes them on purpose. That's why production systems keep rules and unsupervised
layers permanently rather than as a stepping stone. Stripe's own public account is that they
started with logistic regression and added complexity as data volume justified it."
"Where sequence models would genuinely help is the slow-drip case, because that failure is
specifically about needing the account's transaction sequence rather than point-in-time features.
That's the roadmap item."

**Q: Why do you need a graph? Isn't that over-engineering?**
"The RBI built one. MuleHunter.AI, developed by the Reserve Bank Innovation Hub, studies nineteen
mule behaviour patterns and is live at Canara, PNB, Bank of India, Bank of Baroda and AU Small
Finance Bank — 23 banks as of last December. We're building the thing the regulator shipped, plus
the real-time decisioning layer it doesn't have." *(then show the typology matrix)* "APP fraud
has exactly two strong columns, and both of them are graph."

**Q: Why those weights in your scoring?**
"We didn't pick them, we fit them — logistic regression over the log-odds of the model output
plus rule indicators plus graph risk, on a held-out slice. Here's the coefficient table. Three
rules came out near zero because the model already captures their evidence; we kept them anyway
as interpretable overrides."

**Q: How do you handle false positives?**
"By not treating them as a single number. We separate challenge rate from hard-block rate,
because a step-up costs a few seconds and a block costs a customer. Then we set the operating
point on a cost curve rather than a round number." *(show the curve)* "1.8% challenged, and the
dial belongs to the institution."

**Q: Does the model retrain when the analyst confirms fraud?**
"No, and I want to be precise about that. Confirming fraud propagates immediately through the
block list, entity risk scores, the graph, and the consortium — that's the real-time effect you
just watched. Retraining runs on the label store, which is on a lag, because chargeback labels
take 30 to 90 days to arrive. That counter on the governance screen is labels pending maturity.
Anyone claiming instant retraining from a single label is describing something that doesn't work."

**Q: Where does the LLM sit?**
"Strictly downstream of deterministic scoring, and only in two places: phrasing already-computed
structured reasons into readable narrative, and querying our graph tools during investigation. It
never scores, never decides, and can never surface a reason that isn't backed by a real feature
or rule. That boundary is deliberate — an unverifiable fraud explanation is worse than none."

**Q: What about blockchain?**
"One place: the consortium ledger, because that's genuinely multi-party, mutually distrustful,
append-only and low write volume, and signed entries give us provable accountability for bad
reports. Nowhere else. The transaction ledger has a single owner, a sub-second latency budget and
thousands of TPS — consensus there buys latency and throughput problems in exchange for a
property we don't need." *(show the two-column slide)*

**Q: How does this scale?**
"Scoring is stateless and horizontally scalable; every feature is an O(1) lookup against the
profile store rather than a computation, which is what keeps p99 flat under load. The state that
does need care is the streaming aggregate layer, which shards by entity key. The graph is the
genuinely hard part at national scale — that's where you'd move from in-process to a real graph
engine with incremental community detection."

**Q: What would you do next?**
"Sequence models for slow drip. Federated learning across the consortium so institutions share
model improvements rather than just identifiers. And entity resolution — right now a mule with
two accounts at the same bank is two nodes, and it shouldn't be."

---

## 14 — Build order

Ordered by dependency and leverage, not by clock. Each stage is demoable on its own, so you are
never in a state where nothing works.

| # | Build | Why here | Priority |
|---|---|---|---|
| 1 | **Population generator + event schema** | Everything downstream depends on it. 2,000 accounts, payee graph, devices, 90-day warm-up. Highest leverage in the project | Core |
| 2 | **Profile store (L1)** | Streaming aggregates + baselines. Once this exists, every rule and every feature is a lookup | Core |
| 3 | **Ingest → score → decide → WebSocket → table** | The spine. Ugly is fine. If this isn't running you have nothing | Core |
| 4 | **Rule engine, YAML, hot-reload** | 15 rules with reason codes and typology tags. Live authoring is a demo beat | Core |
| 5 | **Attack library + red-team console** | Build attacks *before* the models — they're your test harness and rehearsal loop, not a final flourish | Core |
| 6 | **Graph layer** | Fan-in, forwarding latency, device degree, hop depth, ring detection. Unlocks APP + mule + smurfing at once | Core |
| 7 | **Decision engine + friction budget** | Expected loss, hard rails, cost curve, threshold slider. The RBI story made operable | Core |
| 8 | **Payment surface (PWA)** | QR → payee → amount → PIN, plus step-up and the warning interstitial. Act 1 lives here | Core |
| 9 | **Alert detail + case queue** | Reason bars, baseline chart, beneficiary card, counterfactual. The screen judges look at longest | Core |
| 10 | **Supervised models** | IEEE-CIS + your own, both calibrated, both registered, reliability diagram. Deliberately after the system works — the model is a component, not the product | High |
| 11 | **Investigation workbench** | Entity 360, link graph, money flow, Time Machine | High |
| 12 | **Consortium (two instances)** | Bloom filter first, signed ledger underneath once solid. Act 4 | High |
| 13 | **Governance screen** | Registry, shadow model, drift, label latency, audit chain. Cheap, disproportionate credibility | High |
| 14 | **Narrative writer + investigation agent** | LLM layer, strictly downstream. Built last because everything it describes must exist first | Nice |
| 15 | **Visual system pass + safe mode + rehearsal** | One coherent palette, tabular numerals, projector test, recorded fallback, five timed runs | Core |

> **If everything collapses and you can only ship four things:** the generator, the profile store
> with baseline-relative rules, the APP-scam demo with the warning interstitial, and the alert
> detail screen. That's Act 1 and Act 2 — a judge gets scammed and then sees exactly why the
> system knew. Everything else here is an amplifier on those two acts.

---

## 15 — What ports from Satyum

Satyum (document-integrity engine, SuRaksha Cyber Hackathon 2.0, Canara Bank) already has a
working, tested federation module. Roughly **700 lines of it port to Nazar**, and one piece of
it is better-specified than anything in this playbook.

Worth noting for the pitch: Canara Bank is one of the named MuleHunter.AI deployments (§01). You
built a cross-bank fraud-intelligence layer for their hackathon. That's not a coincidence you
need to explain — it's a line you get to say.

### A — Ports close to verbatim

| Satyum | LOC | What changes for Nazar |
|---|---|---|
| `federation/tokens.py` — HMAC entity tokens + normalisation | 110 | Swap the entity kinds. `pan`, `account`, `phone`, `device` stay identical. Add `vpa`, `psp_handle`. **Drop salted pHash entirely** — there are no images in a payment |
| `federation/registry.py` — report / consult / set membership | 154 | Replace Hamming-distance pHash matching with exact token matching. Keep `banks: set[str]` and `seen_count` — "seen at N banks" is a demo beat |
| `federation/service.py` — report_fraud, consult, advise_from_context | 224 | Same shape. `advise_from_context` reads `ctx.shared`; in Nazar it reads the L1 profile-store lookup already done for scoring |
| Hash-chained audit ledger | — | Already what §10 asks for. Done |
| The 5 federation test files | — | Test *structure* ports; the "Bank A's forgery surfaces at Bank B" end-to-end test becomes "Bank A's confirmed mule surfaces at Bank B" — which is literally Act 4 |

This collapses build item **#12 (consortium)** from a day of work to a port with the entity kinds
swapped. Move it earlier in the order.

The entity normalisation table ports directly and matters more than it looks: it's what makes the
*same* beneficiary tokenise identically at Bank A and Bank B. Without it the consortium silently
never matches. Add a `vpa` normaliser (lowercase, strip whitespace, normalise the handle after
`@`) and an `account` normaliser you already have.

### B — Ports and is better than what I wrote

**The weighted linkage graph with Union-Find ring detection.** This playbook's §05/§06 says "ring
detection" and stops. Satyum's `federation/graph.py` has the actual algorithm, per-identifier ring
weights, a threshold, and weak-signal summing. That fills the gap flagged below.

The weight table maps almost one-to-one onto payments:

| Satyum identifier | Weight | Nazar equivalent | Weight | Rationale |
|---|---|---|---|---|
| `payout_account` | 1.0 | `creditor_account` / beneficiary VPA | 1.0 | Same beneficiary across unrelated payers is near-dispositive |
| `pan` | 1.0 | `pan` | 1.0 | Unchanged |
| `device` | 0.9 | `device_id` | 0.9 | Same device across accounts at different banks |
| `account` | 0.9 | `debtor_account` | 0.9 | Same source account |
| `phone` | 0.7 | `phone` | 0.7 | Unchanged |
| `employer` | 0.4 | — | — | No payments equivalent; drop |
| `ifsc` | 0.3 | `ifsc` / `psp_handle` | 0.3 | Weak — millions share a PSP |
| — | — | `asn` | 0.3 | New. Weak alone, meaningful summed with device |
| — | — | `geo_cell` | 0.2 | New. Weakest |

Keep the **discrimination principle verbatim** — it's the single best defence of the graph layer
in Q&A: *a shared PSP handle alone (0.3 < threshold 1.0) does NOT trigger a ring. A shared
beneficiary account alone (1.0) does. Two weak signals sum: device (0.9) + ASN (0.3) = 1.2 → ring.*

That sentence is the answer to "isn't your graph going to flag everyone who banks with the same
PSP?" — and without it, that question is fatal.

Union-Find connected components, `min_ring_size` default 3, `ring_weight_threshold` default 1.0,
producing `RingEvidence` with a human-readable explanation. Port it as-is.

### C — Ports with adaptation: the advisory firewall

This is the most valuable idea in the Satyum document and it does **not** transfer directly.
Understanding why is the point.

Satyum's rule is *"the model reads; deterministic rules decide"* — a learned signal can only move
`APPROVED → REVIEW`, never auto-approve, never auto-reject, and the deterministic subscore is
preserved unchanged. Nazar's §07/§08 thesis is the opposite: a **calibrated probability drives
expected loss**, so the model *is* in the decision path by design.

Both are correct, for different reasons. Satyum decides on a document where a wrongful reject is a
fairness and legal problem and there's a human in the loop by default. Nazar decides in 38ms on a
payment where graduated friction is the entire product. Copying Satyum's firewall wholesale would
gut §08.

**What to port is the firewall's shape, scoped to the layers where it applies:**

- **Consortium hits and novelty scores become advisory-only.** They can escalate friction
  (`ALLOW → STEP-UP`, or route to the Unknown Pattern queue) but can never *reduce* friction and
  can never hard-`BLOCK` on their own authority. The calibrated model and the deterministic rails
  keep the block decision.
- **Monotone in friction.** State it as an invariant: an advisory signal can only ever add
  friction, never remove it. That is provable, testable, and directly answers *"what if another
  bank falsely reports my customer's payee?"* — worst case, that customer gets a step-up they'd
  otherwise have skipped. Nobody's payment is wrongly blocked by a foreign institution's claim.
- **Mandatory explanation, structurally enforced.** Port `AdvisorySignal`'s contract verbatim:
  every advisory MUST carry a non-empty human-readable explanation, and an opaque *"the model said
  91%"* is rejected at the type boundary. This is the enforcement mechanism behind §09's "reason
  codes are infrastructure" — right now that's a principle in prose; Satyum makes it a contract.
- **Fail-open.** No admissible advisory → the decision is byte-for-byte unchanged. Port the
  invariant and the test.

The resulting Q&A answer is strong and it's one almost no team can give:

> "A compromised consortium member can't block your payment. Foreign advisories are monotone in
> friction — they can only add a step-up, never take one away, and never block on their own.
> Worst case a false report costs a customer three seconds. That's a structural guarantee in the
> type system, not a policy we promise."

### D — Does not port

- **Salted pHash.** XOR-preserving-Hamming is elegant and irrelevant — there are no perceptual
  hashes in a payment stream. Don't carry it over looking for a use.
- **The three-tier "model reads, rules decide" trust model as a whole.** See §C. Nazar deliberately
  puts a calibrated model in the decision path.
- **"Federated learning" as a headline claim.** Satyum's own doc is honest that the coordinator,
  secure aggregation and robust aggregation are roadmap, not built. Nazar's consortium shares
  *confirmed-fraud identifiers*, not model updates — that is **federated fraud intelligence**, not
  federated learning. Keep the distinction sharp. Say "federated learning" on stage and the first
  question is "where's your coordinator?"

### E — Language discipline to carry across

Satyum's doc gets two things right that most projects get wrong, and both are stated *in the code*:

- **It does not say PSI.** The doc explicitly flags that salted-hash / tokenised membership is not
  DH/OPRF Private Set Intersection, and that query-hiding is a later stage. Carry that exactly. In
  Nazar, say **"tokenised set membership"**. If a crypto-literate judge hears "PSI" and then sees an
  HMAC lookup, you've lost more than you gained.
- **It names its own limitations in the artefact, not in the Q&A.** Same discipline as §11's "show
  the failure" and §10's blockchain-scoping slide. This is now a consistent practice across two of
  your projects — say so.

### F — The DPDP argument, which strengthens §10

Satyum's federation driver was the DPDP Act, data localisation, and competitive trust between
banks. That argument applies to Nazar's consortium unchanged, and it's stronger than the framing
currently in §10:

> "Customer financial data cannot leave the bank's perimeter — DPDP, RBI localisation norms, and
> the plain fact that Canara will not hand HDFC its customer records. So we send the fingerprint,
> not the record. The wire payload is an HMAC token and a signature." *(show the wire)*

### G — What this does to the build order

| Item | Was | Now |
|---|---|---|
| #6 Graph layer | Core, unspecified | Core, **algorithm specified** — Union-Find + weight table from §B |
| #12 Consortium | High, day-scale build | **Port**, ~700 lines with entity kinds swapped. Move ahead of #10 |
| #13 Governance | High | Hash-chained audit ledger already exists. Shrinks |
| §13 roadmap answer | Three bullets | Satyum's ADR-005 roadmap — coordinator, secure aggregation, Krum/trimmed-mean, DP noise, FedProx for non-IID, model version pinned in the audit chain, DH/OPRF query-hiding. Specific, and written down before you were asked |

---

## Known gaps in this document

Flagged honestly so nobody builds assuming they're covered:

- **~~§05 is a diagram, not a spec.~~** *Closed by [ARCHITECTURE.md](ARCHITECTURE.md)* — Redis
  key layout, feature catalogue, Postgres DDL, API contract, latency budget, degradation ladder.
  Still open: the 15 rules are specified as a format, not written.
- **~~The graph layer is one bullet.~~** *Closed by §15B and
  [ARCHITECTURE.md §07](ARCHITECTURE.md#07--graph-layer)* — edges, continuous metrics, ring
  detection, weights, and the resolution that graph metrics are features inside `p_ml` rather
  than a separate scored term.
- **No UI specification.** No screen inventory, no layouts, no visual system — despite presentation
  being the stated top priority. **This is now the largest remaining gap.**
- **The demo has no failure branches.** Act 1 depends on a judge scanning a QR on a phone that has
  to connect.
- **All metrics are placeholders.** 94% / 1.8% / 43% / 38ms are illustrative. Replace with real
  red-team output before anyone says them out loud.

---

## Sources

- [Fraud Detection Handbook — transaction simulator](https://fraud-detection-handbook.github.io/fraud-detection-handbook/Chapter_3_GettingStarted/SimulatedDataset.html)
- [ULB credit card fraud dataset](https://www.kaggle.com/datasets/mlg-ulb/creditcardfraud)
- [IEEE-CIS fraud detection dataset](https://www.kaggle.com/c/ieee-fraud-detection/data)
- [Amazon Fraud Dataset Benchmark — IEEE-CIS statistics](https://arxiv.org/pdf/2208.14417)
- [amazon-science/fraud-dataset-benchmark](https://github.com/amazon-science/fraud-dataset-benchmark)
- [RBI Authentication Mechanisms for Digital Payment Transactions Directions, 2025](https://www.lawrbit.com/article/rbi-digital-payment-authentication-guidelines/)
- [Khaitan & Co — analysis of the 2025 Directions](https://www.khaitanco.com/thought-leadership/RBI-Authentication-Mechanisms-for-Digital-Payments-Transactions-Directions)
- [RBI 2FA directives — SMS OTP phase-out](https://www.corbado.com/blog/rbi-2fa-directives)
- [RBI e-mandate limit raised to ₹15,000](https://www.business-standard.com/article/finance/new-e-mandate-guidelines-rbi-enhances-limit-for-e-mandates-on-credit-debit-cards-to-rs-15-000-122060800417_1.html)
- [RBI e-mandate limit to ₹1,00,000 for select categories](https://www.business-standard.com/amp/economy/interviews/rbi-raises-limit-of-e-mandates-for-recurring-online-transactions-to-1-lakh-123120801110_1.html)
- [RBI pilots MuleHunter.AI](https://www.fintechfutures.com/ai-in-fintech/reserve-bank-of-india-pilots-new-mulehunter-ai-solution-to-help-identify-mule-accounts)
- [RTI: 23 banks implementing MuleHunter.AI](https://www.medianama.com/2025/12/223-rti-23-banks-mulehunter-mule-accounts/)
- [NPCI UPI FAQs](https://www.npci.org.in/what-we-do/upi/faqs)
- [UPI beneficiary cooling period](https://sbi.bank.in/web/yono/blog/understand-upi-transaction-limits-cooling-period-and-payment-tips)
