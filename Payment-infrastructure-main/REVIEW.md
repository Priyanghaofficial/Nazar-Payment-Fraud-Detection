# Adversarial Review — Nazar (PLAYBOOK.md + ARCHITECTURE.md)

**Reviewer stance: hostile.** Every claim treated as false until the document proves it.
Every code block treated as something that must compile and run correctly.

**Verdict up front.** The documents are rhetorically excellent and technically unbuildable as
written. They read as if written by someone who has read a great deal about production fraud
systems and has not built one. The prose repeatedly borrows the credibility of national-scale
production while making prototype-scale engineering choices, and the two documents contain a
self-audit section (`ARCHITECTURE §20`, "three found live bugs") that functions as a rhetorical
device rather than a control — every finding below survived it.

Counts: **17 S1 (fatal — wrong decisions, or does not run)**, **23 S2 (serious)**,
**14 S3 (unsupportable claim / demo risk)**.

Severity key:

| | Meaning |
|---|---|
| **S1** | Produces incorrect decisions about real money, or the code as specified cannot execute |
| **S2** | Design is unsound, unspecified where it must be specified, or self-contradictory |
| **S3** | Claim cannot be defended, or the demo beat contradicts the architecture |

---

## Contents

- [1 — The measurement is wrong, so the headline number is meaningless](#1--the-measurement-is-wrong)
- [2 — The ML pipeline](#2--the-ml-pipeline)
- [3 — Anomaly / novelty detection](#3--anomaly--novelty-detection)
- [4 — Graph and ring detection](#4--graph-and-ring-detection)
- [5 — Profile store](#5--profile-store)
- [6 — Decision engine](#6--decision-engine)
- [7 — Consortium and trust](#7--consortium-and-trust)
- [8 — Data model](#8--data-model)
- [9 — Resilience and operations](#9--resilience-and-operations)
- [10 — Security and compliance](#10--security-and-compliance)
- [11 — Claims that will not survive Q&A](#11--claims-that-will-not-survive-qa)
- [12 — Stubs, undefined functions, and hardcoded checks](#12--stubs-undefined-functions-and-hardcoded-checks)
- [13 — What is actually good](#13--what-is-actually-good)

---

## 1 — The measurement is wrong

### F-01 · S1 · The latency number excludes the only place latency comes from

`ARCHITECTURE §13` states the budget is *"measured ingest→decision."* `ARCHITECTURE §03`
implements it:

```python
received_at_ns: int
def elapsed_ms(self) -> float:
    return (time.perf_counter_ns() - self.received_at_ns) / 1e6
```

`perf_counter_ns()` is a monotonic counter with an arbitrary origin. It is only meaningful when
subtracted from another `perf_counter_ns()` sample **taken in the same process**. So this
measures **handler service time**, not ingest→decision.

If the event arrived via the Redis Stream consumer group (`ingest/stream.py`, and the L0 box in
`PLAYBOOK §05`), then everything that matters — **queueing delay in the stream** — is outside the
measurement. Under load, a consumer group that is 400ms behind will still report `13ms`. This is
textbook coordinated omission: the system reports service time and calls it latency, and the
number gets *better* as the backlog gets worse, because a backlogged consumer processes
pre-warmed, cache-hot batches.

If instead `received_at_ns` is a wall clock (which is what the field name says), then subtracting
a monotonic counter from it yields garbage.

**Either way, `p99_decision_ms` — the metric `PLAYBOOK §08` puts "on the wall" and the `38ms` in
Act 1 — is not measuring what the documents claim.**

**Required:** define the measurement point as `event.accepted_at` (wall clock, stamped once at the
trust boundary by the ingesting process) → `decision.emitted_at`, and separately report
`queue_delay`, `service_time`, and `total`. Publish all three. See `docs/01-LATENCY-RESILIENCE.md §1`.

### F-02 · S1 · The two documents describe two different topologies and neither notices

- `PLAYBOOK §05` and `ARCHITECTURE §02` put a **Redis Stream consumer group** between ingest and
  scoring. That is asynchronous.
- `ARCHITECTURE §12` defines `POST /v1/transactions → "ingest + score + decide, returns Decision
  (the hot path)"`. That is synchronous request/response.

These cannot both be the hot path. If the stream is on the path, the HTTP handler must block on a
correlated stream round trip — a mechanism that appears nowhere, and which adds a full
enqueue/dequeue cycle to every decision. If the stream is *not* on the path, then the L0 box in
the architecture diagram is decorative and the `ingest→decision` measurement point is undefined.

This is not a documentation nit. **It determines whether the system can meet its own latency
target**, and it determines the failure semantics when the scorer is down.

### F-03 · S2 · The budget is a sum of medians presented as a tail budget

`PLAYBOOK §03.6` correctly states *"p99 matters and p50 is close to meaningless."*
`ARCHITECTURE §13` then builds a table of single-value stage budgets, sums them to `~13 ms`, and
labels the result a constraint on `p99 < 50ms`.

**p99 is not the sum of p50s, and it is not the sum of p99s either.** For k independent stages,
the composed p99 is bounded below by `max(p99_i)` and above by something much worse than
`Σ p99_i` once you account for correlated stalls (GC, page cache miss storms, Redis fork-on-BGSAVE,
TCP retransmit at 200ms granularity). A p99 budget must be allocated per stage *as a p99*, with
measured evidence, and must include the stalls that are common-mode across stages.

### F-04 · S2 · The stage numbers were not measured, and two are wrong by more than an order of magnitude

| Stage | Doc's budget | Realistic |
|---|---|---|
| Deserialise + validate (Pydantic v2, ~25 fields) | 1 ms | 5–25 µs |
| GBM inference (1 row, `num_threads=1`) | **3 ms** | **2–50 µs** for a 300-tree LightGBM |
| Profile load (1 Redis pipelined RTT, same AZ) | 5 ms | 0.15–0.4 ms p50, 1–4 ms p99 |

A 3ms budget for a single-row LightGBM predict is off by roughly 100×. A 5ms budget for one
same-AZ Redis pipeline is off by roughly 15×. The stated total (`~13 ms`) and the demo's `38ms`
and the target (`p50 < 15ms`) are three mutually inconsistent numbers. **`PLAYBOOK`'s own "Known
gaps" section admits all metrics are placeholders; `ARCHITECTURE §13` presents the same numbers as
"a design constraint, not an aspiration."** The documents disagree about the epistemic status of
their own headline figures.

### F-05 · S1 · "Fire-and-forget after the response" puts the deferred work back on the critical path

`ARCHITECTURE §13`: *"Persist, graph write, WS fan-out — Off the critical path. Fire-and-forget
after the response."*

In a single-process asyncio server, `asyncio.create_task(...)` schedules work **on the same event
loop** that must serve the next request. It is off the *response* path and squarely on the
*latency* path. Under load this deferred work is precisely what generates the p99 tail the design
claims to defend.

Compounding it: there is **no backpressure design anywhere**. What happens when the persist queue
grows faster than Postgres drains it? Unbounded queue → OOM → the process dies holding undelivered
decisions. Bounded queue → dropped decisions → a hash-chained audit log with holes in it, which is
worse than no audit log because it looks complete.

---

## 2 — The ML pipeline

### F-06 · S1 · The production model is trained on data the team generated, and the detection rates measure the generator

This is the central flaw and everything downstream inherits it.

`PLAYBOOK §01` demolishes the Fraud Detection Handbook simulator with an argument that is exactly
correct:

> *"Tooth one — the labels are a rule you can read. Scenario 1 **is** a threshold. Train XGBoost on
> this and it will learn `amount > 220` and post a spectacular AUC-PR. That number measures
> nothing."*

`PLAYBOOK §02` then makes the team's own generator the **production training set and the source of
every quoted metric**, and the only defence offered is:

> *"Each typology is generated by an agent with a strategy, not a rule."*

An agent with a strategy is a stochastic rule with more parameters. A 500-tree gradient boosting
ensemble will recover it. **`94% on mule fan-out, 91% on ATO, 43% on slow drip` are measurements of
how well LightGBM can invert code the same team wrote.** They are not detection rates. The `43%`
"honest failure" beat is equally determined: whoever wrote the slow-drip agent chose 43%.

The playbook is aware of this failure mode in the abstract and commits it in the concrete, one
section apart, without noticing.

**Required:** an explicit epistemic policy separating what the generator is for (load, integration,
demo, red-team rehearsal, invariant testing) from what it may never be used for (claiming detection
performance). See `docs/03-ML-PIPELINE.md §2`.

### F-07 · S1 · Calibration is calibrated to a base rate the team chose, which invalidates every rupee figure in the decision engine

`PLAYBOOK §07 Step 1`: *"among transactions scored 0.30, roughly 30% are actually fraud."*
30% **of generator traffic**. The generator's fraud prevalence is a free parameter the team set.

Every number downstream is a linear function of that parameter:

```
el = p_final × amount × LOSS_GIVEN_FRAUD[rail]
```

Real UPI fraud prevalence is on the order of 10⁻⁴–10⁻³ of transaction count. If the generator runs
at, say, 2% — a typical choice, because otherwise you cannot train — then every `p_final` is
inflated by roughly 20–200×, therefore every expected loss is inflated by 20–200×, therefore
**the ₹50 / ₹500 / ₹5,000 ladder in `ARCHITECTURE §09` sits 1.5–2.5 orders of magnitude off**, and
`challenge_rate = 1.8%` and `value_recall = 94%` are artefacts of the prevalence knob.

The documents present calibration as the thing that makes the economics defensible. It is the thing
that makes them arbitrary, because the input is invented.

**Required:** prevalence correction as an explicit, documented log-odds shift, plus a sensitivity
sweep showing decision quality across the assumed production prevalence. See
`docs/03-ML-PIPELINE.md §4.3`.

### F-08 · S1 · `scale_pos_weight` and isotonic calibration interact, and the interaction is unaddressed

`PLAYBOOK §04` keeps `scale_pos_weight` over SMOTE (correct) and `PLAYBOOK §07` fits isotonic "on
a held-out slice."

`scale_pos_weight` reweights the positive class, so the booster's raw output is **no longer an
estimate of `P(y=1|x)` at the natural prevalence**. That is fine and expected — calibration exists
to fix it. But it is only fixed if the calibration slice **preserves the natural class balance**.
Nothing in either document says so. If the held-out slice is drawn from the rebalanced training
frame (the default thing that happens when you `train_test_split` after resampling), `p_ml` is
calibrated to the wrong base rate and every expected loss is wrong by the reweighting ratio.

This is the single most common way this exact stack (LightGBM + `scale_pos_weight` + isotonic +
expected loss) fails in production, and neither document mentions it.

### F-09 · S2 · Isotonic regression is the wrong calibrator for this positive count and produces unusable step functions

Isotonic is non-parametric, needs many positives, cannot extrapolate beyond the calibration range,
and produces a **step function with wide flat regions** when positives are sparse. In a flat region,
thousands of distinct raw scores collapse to one probability — which destroys exactly the ordering
that `expected_loss = p × amount` depends on, and makes the reliability diagram (the doc's proudest
artefact) look artificially perfect while carrying no resolution.

With a few thousand synthetic positives you will get perhaps 20–40 distinct output values.

**Required:** beta or Platt calibration as primary (smooth, extrapolates, few parameters), isotonic
only with bootstrap averaging and only above a stated positive count. See `docs/03-ML-PIPELINE.md §4.2`.

### F-10 · S1 · The offset-constrained rule regression cannot be fit as described, and should not exist

`ARCHITECTURE §08 / §17`, `PLAYBOOK §07`:

> *"a logistic regression of the label on `[logit(p_ml), rule_1, …, rule_n]` … with `logit(p_ml)`
> offset-constrained to coefficient 1.0"*

Three separate problems, in increasing severity:

1. **It is not implementable in the obvious library.** `sklearn.linear_model.LogisticRegression`
   has no offset/exposure parameter. You need `statsmodels.GLM(family=Binomial(), offset=...)` or a
   hand-rolled fit. A doc that calls this the mechanism which makes the double-counting answer
   *"true rather than hopeful"* should name the tool.

2. **It will not converge.** A high-precision rule that fires 100 times in the holdout with 100%
   fraud is **complete separation**: the MLE is `+∞`. No regularisation is specified. This will
   literally fail to fit, or produce a coefficient of 40 with an infinite standard error, which
   then multiplies into `logit_final`. Rules are *designed* to be high-precision, so this is the
   expected case, not the edge case.

3. **The whole construction is a no-op, and the documents misread their own result.** Each
   `1[rule_i fired]` is a deterministic threshold on features the GBM already consumes. A tree
   ensemble can represent an axis-aligned threshold indicator **exactly**. So conditional on a
   well-fit `p_ml`, the incremental log-odds of essentially *every* rule is ≈0 — not three of
   fifteen. `PLAYBOOK §07` frames "three of our fifteen fit to zero" as a satisfying discovery; the
   correct expectation is that **all of them should fit to zero, and any that do not are evidence
   the GBM is underfit.** The documents have built an entire fitted-composition subsystem whose
   own logic predicts it does nothing.

The real reason to keep rules is **policy, auditability, and regulator-facing determinism** — the
documents say this in passing and then build a machine that contradicts it.

**Required:** delete the composition layer. Rules become either (a) rails — deterministic,
unweighted, versioned, score-independent — or (b) boolean **features into the GBM**, which makes
double-counting impossible by construction. `p_final ≡ p_ml`. See `docs/03-ML-PIPELINE.md §5`.

### F-11 · S1 · Hot-reloading rules destroys calibration, and the demo beat depends on hot-reloading rules

`PLAYBOOK §07 Step 4` makes live rule authoring a headline demo beat: *"a judge says 'what if I want
to catch X,' you open the file, type a rule, save it, and it fires on the next transaction while
they watch."*

But under `logit_final = logit(p_ml) + Σ wᵢ·1[ruleᵢ]`, a newly authored rule has **no fitted
weight**. What is `w` for it? Undefined. Whatever you pick, the moment it fires:

- `p_final` is no longer calibrated
- `expected_loss` is no longer a rupee quantity
- the ₹50/₹500/₹5,000 thresholds no longer mean anything
- the reliability diagram on the governance screen is now stale and wrong

**The signature demo beat and the signature technical claim are mutually exclusive, and neither
document notices.** Under the fix in F-10, live authoring survives — as authoring a *rail*, which
is honest and has no calibration consequence.

### F-12 · S1 · Training-data survivorship: P7 blocks the direct feedback path and leaves the indirect one wide open

`ARCHITECTURE P7` / Golden Rule 5: *"No feature is derived from Nazar's own prior decisions."*
Property-tested. Good, and correct as far as it goes.

`ARCHITECTURE P2`: training data comes from the persisted feature vectors of past decisions.

**Those past decisions determined which outcomes exist.** Blocked transactions have no outcome.
Held transactions have no outcome. Step-up-abandoned transactions have no settled amount. Trusted-
pair fast-path transactions were never scored at all — 70–80% of traffic by the document's own
claim — so they contribute nothing to training and their absence is not random.

So the training label distribution is a function of the deployed policy, and retraining closes a
feedback loop **through the data** rather than through the feature vector. P7 nails the door shut
and leaves the window open. Within a few retrain cycles the model has learned the policy's blind
spots as if they were properties of the world.

This is standard, well-known, and the documents are silent on it while claiming to have
*"eliminated training/serving skew structurally rather than by discipline."*

**Required:** a randomised control holdout (traffic decided by a fixed baseline policy regardless
of score, excluding regulatory rails), propensity logging on every decision, and off-policy
evaluation. See `docs/03-ML-PIPELINE.md §7`.

### F-13 · S2 · The card rail has no model and the documents contradict themselves about it

- `PLAYBOOK §02` / `§13`: *"Our card-not-present model is trained on IEEE-CIS."*
- `PLAYBOOK §02` table: IEEE-CIS is **"In the live path? No."**
- `ARCHITECTURE §11`: `rail TEXT NOT NULL -- UPI | IMPS | CARD`
- `ARCHITECTURE §09`: `LOSS_GIVEN_FRAUD` has a Card (CNP) row with a full rationale

So card transactions exist in the schema, have their own loss-given-fraud economics, and flow
through the decision engine — scored by what? Either:

- a model trained on IEEE-CIS, which is **exactly the schema-mismatch sin the playbook spends a
  page condemning** (`PLAYBOOK §04`: *"I proposed training on IEEE-CIS and scoring live phone
  payments without noticing those are incompatible schemas. One question destroys it."*), or
- nothing, in which case the card rail and its LGF row are decorative.

There is no adapter from `TransactionEvent` to IEEE-CIS's 431 columns, **and there cannot be**:
V1–V339 are undocumented Vesta-proprietary features. The mapping does not exist even in principle.

### F-14 · S2 · Rail-specific models with no sample-size analysis

`ARCHITECTURE §20.6` makes "one canonical event, rail-specific models" the coherent resolution.
Splitting an already-small synthetic positive population three ways is not analysed anywhere. If
the generator produces, say, 5,000 frauds over 90 days, a per-rail split leaves each model with
low-thousands of positives — below the threshold where isotonic calibration (F-09) is viable and
where per-typology metrics carry meaningful confidence intervals.

### F-15 · S2 · PSI is the only drift signal, and it does not detect the drift that matters

`ARCHITECTURE §02` (`govern/drift.py — PSI`), `PLAYBOOK §10`: *"PSI on the top features, one chart,
one threshold line."*

PSI on **feature marginals** detects population shift. Adversarial fraud is **concept drift**: the
joint `P(y|x)` changes while the marginals stay put, because the adversary is deliberately staying
inside the observed distribution. PSI is structurally blind to it. Additionally, PSI on a feature
with `NaN` (the cold-start case the design deliberately creates) is undefined, and PSI is unstable
under low bucket counts.

Missing entirely: score-distribution monitoring, rule-firing-rate monitoring, per-typology backtest
on a rolling window, and calibration drift (the reliability diagram recomputed on recent matured
labels — which is the one that actually invalidates the expected-loss thresholds).

### F-16 · S2 · No adversarial-ML section at all, in a system whose adversary controls most of the feature vector

The documents cover prompt injection via the UPI remittance field (`ARCHITECTURE §20.5` — genuinely
good, credit given). They cover nothing else.

Classify the feature catalogue by who controls it:

| Class | Features |
|---|---|
| **Fully attacker-controlled** | `amount`, timing (→ all velocity + `hour_rarity` + `geo_jump_kmh` Δt), `device_id`, `asn`/IP (VPN), `initiation`, `remittance_info`, and — via a burner account — the entire payee side |
| **Attacker-shapeable over days** | `payee_fanin_*`, `payee_fwd_*`, `pair_txn_count_90d`, `account_age_days`, `device_age_hours`, ring structure |
| **Bank-observed, hard to forge** | payer's genuine 90-day baseline, settlement outcomes, matured labels |

**The overwhelming majority of the feature vector is attacker-controlled or attacker-shapeable.**
Nothing in either document addresses:

- **Model extraction via free oracle.** Every payment returns a friction level. A fraudster sends
  ₹1 to a mule and reads the response to learn whether it is burned. Unlimited queries,
  ground-truth labels, no rate limit specified anywhere. This is a *reconnaissance API* the design
  ships by default.
- **Evasion.** `PLAYBOOK §02`'s own typology table lists an "evasion knob" per attack ("amount just
  under the victim's own p95", "forwarding delay", "inter-arrival jitter") — the documents
  enumerate the evasion strategy and then never defend against it.
- **Poisoning through analyst dispositions**, which feed the label store, blocklists, and graph.
- **Monotonicity.** LightGBM supports monotone constraints. Applying them to features whose risk
  direction is known (`amount` ↑, `payee_fanin` ↑, `payee_age` ↓) is cheap, improves calibration
  stability, and bounds how far a single manipulated feature can move the score. Not mentioned.

---

## 3 — Anomaly / novelty detection

### F-17 · S1 · The novelty module's output does not exist

`ARCHITECTURE §02`: `novelty.py — IsolationForest + robust z`.
`ARCHITECTURE §08 Stage 3`:

```python
novel = novelty_z > NOVELTY_HIGH and p_ml < MODEL_LOW
explanation=f"Feature vector is {novelty_z:.1f} MAD from any training cluster; ..."
```

**`IsolationForest` does not produce a `z`, does not produce a MAD, and has no notion of a
"training cluster."** Its output is an average-path-length-derived anomaly score, roughly in
`[-0.5, 0.5]` for sklearn's `score_samples`, with no distance semantics whatsoever.

The explanation string describes a **Mahalanobis or kNN-distance detector**. So either the module is
an isolation forest and the flagship "zero-day" explanation shown to analysts and judges is
fabricated, or it is a distance detector and the architecture names the wrong algorithm in three
places.

Given `ARCHITECTURE P6` — *"A signal that cannot explain itself cannot cross a boundary"* — shipping
an explanation string that describes a different algorithm than the one that ran is the exact
failure P6 exists to prevent.

### F-18 · S1 · IsolationForest crashes on every cold-start transaction

`ARCHITECTURE §06`: *"A first-ever transaction has no baseline. Do not impute to zero and do not
impute to the global mean … Pass `NaN`, and emit a `COLD_START` reason code."* — correct for
LightGBM, which handles `NaN` natively.

`sklearn.ensemble.IsolationForest` calls `check_array(..., force_all_finite=True)`. **It raises
`ValueError` on `NaN`.** No imputation strategy for the novelty lane is specified anywhere.

So: every new account's first transaction, every branch-initiated NEFT (no `device_id`), every card
authorisation (no beneficiary VPA) throws inside the novelty signal. Best case the ensemble catches
it and marks the lane degraded on the entire cold-start population — which is precisely the
population novelty detection is *for*. Worst case it 500s the hot path.

This is a concrete, day-one, reproducible crash.

### F-19 · S1 · `squash()` and `_escalation()` are undefined, and they are the functions that add friction to real customers

`ARCHITECTURE §08`:
```python
suspicion=squash(novelty_z),
```
`ARCHITECTURE §10`:
```python
steps = max(_escalation(a.suspicion) for a in admissible)
idx   = min(FRICTION_LADDER.index(d.action) + steps, len(FRICTION_LADDER) - 1)
```

Neither `squash` nor `_escalation` is defined anywhere in either document.

Trace the chain: an undefined statistic (`novelty_z`, F-17) is passed through an undefined squashing
function to produce `suspicion`, which is passed through an undefined escalation function to
produce the **number of rungs a real customer is moved up the friction ladder**.

`ARCHITECTURE §10` is presented as the system's strongest safety guarantee — *"That is enforced in
the type system, not promised in a policy"* — and its load-bearing arithmetic is two stubs.

### F-20 · S1 · An advisory can escalate ALLOW → HOLD, so the flagship safety claim is false

The claim, repeated verbatim in both documents and offered as the answer to the hardest question in
the deck:

> *"Worst case a false report costs a customer three seconds. That is enforced in the type system,
> not promised in a policy."*

The ladder:

```python
FRICTION_LADDER = [ALLOW, ALLOW_MONITOR, STEP_UP, STEP_UP_INTERSTITIAL, HOLD]
```

`HOLD` is on the ladder. `min(index + steps, len-1)` clamps to `HOLD`, not to `STEP_UP`. With
`_escalation` undefined (F-19), nothing bounds `steps`. So a single foreign advisory can move a
transaction from `ALLOW` to `HOLD` in one hop.

**A HOLD on an irreversible push rail is a block from the customer's point of view** — the money
does not move, and it does not move until an analyst works the queue. That is hours, not three
seconds. The document's own §14 acknowledges holds queue.

The invariant the documents want is *"an advisory may never raise the friction level above
STEP_UP_INTERSTITIAL"*, which is a two-line change and is not what is written.

### F-21 · S2 · `attach_advisory`'s admissibility filter is dead code

```python
admissible = [a for a in advisories if a.explanation and a.explanation.strip()]
if not admissible:
    return d                                    # (4) fail-open
```

`SignalFinding.__post_init__` **already raises** on empty or whitespace explanations. So no
`SignalFinding` can exist with an inadmissible explanation, so `admissible == advisories` always,
so invariant (4) "fail-open" can never trigger, and `test_advisory_fail_open` (`ARCHITECTURE §18`)
passes vacuously — it can only be made to fail by constructing an object the constructor forbids.

There is **no actual admissibility check**: no reporter-reputation gate, no staleness bound, no
signature verification, no confidence floor. The safety property is enforced by a filter that
cannot filter.

### F-22 · S2 · Novelty routes to customer friction with zero measured precision, contradicting the document's own diagnosis

`PLAYBOOK §04`: *"Isolation Forest at 25% is far too heavy for a signal with that precision
profile."* — correct diagnosis of IF's precision.

Then it is wired directly to the friction ladder as an advisory, escalating real customers, with
**no precision estimate anywhere in either document**.

A model that by construction cannot distinguish *rare* from *bad* now adds friction to: the
customer's first payment after moving city, their first payment after a new job changed their
salary date, their first large purchase, their first payment on a new phone. These are the highest-
lifetime-value moments in a retail banking relationship.

**Required:** novelty routes to an investigation queue only. It touches customer friction only
after its precision at the operating threshold has been measured on matured labels and published.

### F-23 · S2 · The zero-day demo beat is rigged and is presented as evidence

`PLAYBOOK §07 Step 3` / Act 5: *"Invent a brand-new typology on the spot … The supervised model
shrugs. The novelty detector fires."*

The "brand-new typology" is drawn from the team's own generator, whose parameter space the team
controls. Placing a new typology far from the training manifold in feature space is trivial — it is
a slider. **The beat is guaranteed to succeed for reasons that have nothing to do with the
detector's real-world capability**, and both documents present it as demonstrating generalisation
to unseen attacks.

Every judge who has trained an anomaly detector knows that a detector trained on distribution D
flags *everything* not in D — including all legitimate behaviour not in D. The unanswered question
is the false-positive rate on real out-of-distribution *benign* traffic, and it is never asked.

### F-24 · S3 · `payee_fanin_accel` is mostly a proxy for payee age

```
payee_fanin_accel = fanin_1h / (fanin_24h/24 + ε)
```

For a payee first seen an hour ago, `fanin_24h == fanin_1h`, so `accel = 24` regardless of the
actual burst. Every brand-new payee maxes this feature. It is `payee_age` wearing a different hat,
and it is collinear with `payee_age_days` and `payee_is_new`, both already in the catalogue. The
`NOT_APPLICABLE` guard in `§06` ("require denominator ≥ 1") does not touch this.

### F-25 · S3 · `hour_rarity` saturates and is not a rarity

```
hour_rarity = 1 − hour_hist[h] / max(hour_hist)
```

For any account with concentrated usage, this is ≈1.0 for most of the day. It is
"one minus normalised-by-max", not a rarity. The quantity that carries the signal is
`−log P(hour = h)` or the empirical CDF position. The catalogue reads as if written without ever
plotting the distribution.

---

## 4 — Graph and ring detection

### F-26 · S1 · The ring weight table is imported from a domain where the identifiers mean the opposite thing, and it is wired to a hard BLOCK

`ARCHITECTURE §07`, ported from Satyum with weights preserved:

| Identifier | Weight |
|---|---|
| `creditor_account` (shared beneficiary) | **1.0** |
| `pan` | 1.0 |
| `device_id` | 0.9 |

`ring_weight_threshold = 1.0`, `min_ring_size = 3`.

So: **any three unrelated payers sharing one beneficiary account form a confirmed ring.**

In Satyum's domain — loan applications sharing a payout account — that is near-dispositive.
In payments, a shared `creditor_account` across unrelated payers is the definition of:

- a merchant
- a utility company
- a school's fee account
- a landlord
- a mutual fund
- **every single legitimate collection account in the country**

And `ARCHITECTURE §09` makes it a hard rail that bypasses the score entirely:

```python
if ctx.graph.ring_confirmed:
    return block("PAYEE_IN_CONFIRMED_RING")
```

**Three people paying the same electricity board are blocked.** This is the most severe finding in
the review. The documents' own defence of the layer —

> *"a shared PSP handle alone (0.3 < 1.0) does not form a ring. A shared beneficiary account alone
> (1.0) does. Without that sentence, 'won't this flag everyone who banks with the same PSP?' is
> fatal."*

— answers the wrong objection with confidence. The fatal question in payments is *"won't this flag
everyone who pays the same merchant?"*, and the answer the documents give is **yes, by design.**

The weight table was ported verbatim as a strength (`PLAYBOOK §15B`: *"maps almost one-to-one onto
payments"*, *"Port it as-is"*) without re-deriving what the identifiers mean in the new domain.

**Required:** identifier weight must be a **function of that identifier's population frequency and
age**, not a constant — a shared beneficiary paid by 10,000 people is a merchant (weight ≈ 0); a
shared beneficiary paid by 11 unrelated people within an hour with no prior history is a signal.
See `docs/05-GRAPH-CONSORTIUM.md §2`.

### F-27 · S1 · "Confirmed" ring is never defined, so unsupervised clustering output blocks customers

`ARCHITECTURE §09` reads `ctx.graph.ring_confirmed` directly off graph metrics computed in L1.
`ARCHITECTURE §10`'s table says "Confirmed ring — No, it's a rail — Locally established."

"Locally established" by whom? If it means the Union-Find algorithm's output, then an **unsupervised
connected-components result hard-blocks payments with no human in the loop**, which contradicts
Golden Rule 4's spirit and every other safety claim in the document. If it means analyst-
dispositioned, then the field must come from Postgres, not from `graph/metrics.py`, and the
architecture says otherwise.

### F-28 · S1 · Union-Find never un-merges, so the graph collapses into one component

Union-Find is monotone by construction. Over a 30-day rolling window, with any weight-1.0
identifier (which shared-beneficiary is, per F-26), the transitive closure collapses. Payer A pays
Merchant M; Payer B pays Merchant M and Merchant N; Payer C pays Merchant N — A, B, C, M, N are one
component. Iterate across 30 days of a real payment graph and **you get one component containing
essentially every active account in the bank.**

There is no time decay, no edge-weight decay, no component-size cap, no re-partitioning, and no
mechanism to distinguish a hub from a ring. `ring_size` then becomes a feature inside `p_ml` whose
value is "the size of the giant component" for every transaction.

### F-29 · S2 · `hops_to_cashout` is unbounded work on the same event loop, over a giant component

```
hops_to_cashout = BFS depth to nearest node with fwd_ratio < 0.1 and high CASH_OUT
```
*"Bound it at depth 3 and compute it asynchronously off the critical path."*

Depth-3 BFS in the giant component of F-28 visits a large fraction of the graph. It is scheduled
"asynchronously", which per F-05 means on the scoring event loop. This is the worst combination:
unbounded work, on the latency-critical resource, triggered by every transaction.

### F-30 · S2 · Staleness of graph features is adversarially exploitable and is not modelled

*"the cached value from the previous event is fresh enough, and the staleness is bounded and
recorded."*

The staleness is **not bounded** — it is a function of load on the async worker. Under load,
staleness grows. Attacks arrive in bursts. Therefore **graph features degrade precisely during an
attack**, and an attacker who can generate load (trivially: card testing, which the system is
supposed to detect) can degrade the graph lane on purpose and then push the real fraud through.

Meanwhile `ARCHITECTURE P1/P2` claim replay is exact. A feature whose value depends on background-
worker scheduling is reproducible only in the sense that you recorded what you happened to get.

### F-31 · S2 · Ring detection has no complexity or memory analysis at any scale

`ARCHITECTURE §07`: *"At a few thousand nodes this is microseconds; at national scale it is the part
that needs a real graph engine."* That is the whole analysis. No node/edge counts, no memory
figures, no incremental-update cost, no rebuild time, no discussion of what happens between "a few
thousand" and "national."

---

## 5 — Profile store

### F-32 · S1 · The document contradicts itself on `fanin`, the single most load-bearing feature in the system

| Location | Operation |
|---|---|
| `ARCHITECTURE §05` key table | *"Distinct counts (`payers`, `accts`) are sorted sets … so **`ZCARD`** after trimming gives the distinct count exactly"* |
| `ARCHITECTURE §05` pipeline | `p.zcard(k.window("payee", ev.creditor_account, "payers"))` |
| `ARCHITECTURE §07` metrics | `fanin_1h = ZCARD w:payee:{a}:payers  (1h window)` |
| `ARCHITECTURE §05` window rule | *"Window read is `ZCOUNT key (now-W) now`"* |

**`ZCARD` ignores scores entirely.** It returns the cardinality of the whole set.

The trim is `ZREMRANGEBYSCORE key 0 (now-MAXWINDOW)` with a **single MAXWINDOW per key**. The
`payers` key declares windows `1h 24h`, so MAXWINDOW = 24h. Therefore `ZCARD` returns the
**24-hour** distinct-payer count, and `payee_fanin_1h` — described in the document as *"the
APP-scam signal"* and *"mule fan-out"*, the feature the entire pitch rests on — is **silently
computed over the wrong window.**

The correct read is `ZCOUNT key (now-3600000) now` over a set whose member is the payer id and
whose score is that payer's most recent payment time. The pipeline sample uses `ZCARD`. The demo
narrative ("11 people have paid it *today*" / "fan-in 11 in the *last hour*") uses both windows
interchangeably in the same act.

### F-33 · S1 · `w:payer:{acct}:amt` is structurally incapable of producing `amt_velocity_1h`

```
zset  w:{entity_kind}:{entity_id}:{metric}
      member = end_to_end_id
      score  = epoch_ms
```
```
| `w:payer:{acct}:amt` | 1h 24h 7d 30d | spend burst (sorted set of amounts, use ZRANGEBYSCORE sum) |
```

A sorted set has **one** score per member. The layout declares `score = epoch_ms`. The `amt` row
declares it a "sorted set of amounts." **You cannot have both**, and you need both to compute a
*windowed sum*.

Additionally: **Redis has no sum-over-score-range operation.** `ZRANGEBYSCORE` returns members;
summing happens client-side. So even with a corrected encoding, `amt_velocity_1h` requires
transferring every transaction in the window over the network and summing in Python — which is
O(k) bytes on the wire, not the O(1) lookup the entire latency argument depends on, and it scales
with exactly the customers who transact most.

Two features in `§06` (`amt_velocity_1h`, `_24h`) and one guard-table row (`amt_over_p95`) depend on
this and cannot be computed.

**Required:** time-bucketed counters (per-minute `HINCRBY` into a bucketed hash, read N buckets),
which are O(window/bucket) integers, exactly summable, trivially expirable, and idempotent-safe.
See `docs/02-DATA-AND-FEATURES.md §3.2`.

### F-34 · S1 · Six features in the catalogue have no backing key in the storage design

`§06` defines the feature catalogue. `§05` defines the key layout. They do not intersect:

| Feature (§06 / §07) | Backing key in §05 |
|---|---|
| `payee_fwd_latency_s` | **none** |
| `payee_fwd_ratio` | **none** |
| `geo_jump_kmh` | **none** (needs prior geo + prior ts) |
| `dormancy_days` | **none** (needs last-txn ts) |
| `asn_is_new` | **none** (needs a per-payer known-ASN set) |
| `pair.p95_amount`, `pair.last_disposition` | **none** (§05 has only `w:pair:…:txn`) |

`payee_fwd_latency_s` and `payee_fwd_ratio` are the two features `§06` singles out:

> *"the two features that separate a mule from a busy merchant, and neither exists in any public
> dataset. This is the concrete reason the generator is item #1 in the build order."*

They have no storage design. `§07` says they are *"computed incrementally on write, cached in the
profile store"* — the key layout table does not contain them.

`pair.p95_amount` and `pair.last_disposition` are read by the **trusted-pair fast path**, which the
document calls *"the single largest lever on both latency and friction"* and which is claimed to
handle 70–80% of all traffic. **The highest-volume code path in the system reads three fields that
do not exist in the storage design.**

The catalogue and the key layout were evidently written independently and never cross-checked.

### F-35 · S1 · "One pipelined round trip" is impossible in Redis Cluster, which is mandatory at the claimed scale

`ARCHITECTURE §05`: *"**This is the design.** One round trip, no fan-out, no N+1."*

The read touches keys for: payer, payee, device, ASN, `(payer,payee)` pair, plus two global
blocklists. Those are **seven different hash slots on up to seven different nodes.** A single Redis
Cluster pipeline cannot span slots — you get `CROSSSLOT`, or the client silently splits it into
per-node pipelines, which is N round trips, not one.

At the scale the document invokes to justify the design (738M/day), a single Redis instance is not
an option. So the design's central claim is false in exactly the regime the design cites as its
justification. `PLAYBOOK §13`'s answer — *"the streaming aggregate layer shards by entity key"* —
does not survive the question **"which entity key?"**, because one decision needs five.

**Required:** hash-tag co-location where possible, and **N concurrent single-slot pipelines** so
wall-clock latency is `max(RTT)` rather than `Σ(RTT)`. Or a server-side Redis Function per entity
group. See `docs/02-DATA-AND-FEATURES.md §3.1`.

### F-36 · S2 · Global blocklist SETs are unshardable hot keys on the critical path

```
set   bl:payee:local
set   bl:payee:consortium
hash  bl:payee:consortium:meta     token → {reporters, first_reported, weight}
```

`SISMEMBER` is O(1), but a single Redis key lives on **one shard**. Every transaction in the system
hits these three keys. They cannot be sharded — that is what "one key" means. At any meaningful
throughput this is a single-node bottleneck, and `bl:payee:consortium:meta` is a single hash with
one field per reported token, i.e. an unbounded single-key structure.

**Required:** replicated local filter (cuckoo/Bloom) in each worker process, refreshed by pub/sub,
with exact confirmation against a sharded per-token key before any rail fires. This also removes
three commands from the hot read and — critically — makes the rails computable with **zero I/O**,
which is what makes a deadline-timeout fallback possible at all (see F-49).

### F-37 · S2 · `ProfileBundle.from_pipeline(await p.execute())` is positional parsing of ~28 results

Redis pipelines return a positional array. Any conditional command — skip the device keys when
`device_id` is absent (which `§04`'s `requires: {"device_id"}` and `§20.3` explicitly require) —
**shifts every subsequent index by one**, silently assigning the ASN count to the device-degree
feature, the baseline hash to the pair counter, and so on.

The resulting bug class is **silent feature corruption**: no exception, no log line, just wrong
numbers flowing into a calibrated probability and a rupee threshold. It is the worst possible bug
class for this system, and the design invites it.

**Required:** a declarative request spec (list of `(name, command, args)`) zipped against the reply
array by name, with a length assertion.

### F-38 · S2 · No memory sizing anywhere

`§05` says *"memory is bounded without a sweeper"* and *"At hackathon population size this is
correct and cheap."* Then `§13` invokes 738M/day.

Neither document contains a single byte count. Order-of-magnitude for the production framing:
7-day payer-txn zsets across ~300M active accounts, plus the `(payer,payee)` pair keyspace with a
90-day window — which is the **cross product** and by far the largest structure in the system —
runs to multiple terabytes of RAM. Nothing in the documents acknowledges this, and the one scaling
mitigation offered (HyperLogLog) addresses distinct counts only.

### F-39 · S2 · HyperLogLog is proposed for exactly the cardinality range where a threshold rule reads it

`§05`: *"At UPI's 738M/day you would swap in HyperLogLog and accept ~0.8% error."*

The rule that consumes this feature is:

```yaml
when: payee.distinct_payers_1h >= 8
```

Fan-in values of interest are 8–20. A relative-error argument (0.8%) is the wrong frame at
cardinality 11 — what matters is whether the sketch returns 7 or 8 when the truth is 8, and that is
a *rule firing or not firing*. Redis HLL uses linear counting at low cardinality so the error is in
fact small, but the document's stated justification is wrong, and **sketching the exact quantity a
threshold rule reads is the wrong architectural instinct** regardless of the error bound.

### F-40 · S2 · Timestamp source is undefined, so every window is corruptible by clock skew

`score = epoch_ms` — from whose clock? The producer's, the API server's, or Redis's? Skewed or
malicious producers corrupt every sliding window in the system, and `ARCHITECTURE §20.2` lists clock
skew as something the integrity checker will *detect* rather than something the design *prevents*.

### F-41 · S2 · Point-in-time read/write ordering is unspecified

The window read must exclude the current event to be a valid point-in-time feature. The trim
happens on write. So either you write-then-read (self-inclusion, and the feature now includes the
transaction it is scoring) or read-then-write (untrimmed set, F-32). The document never states the
ordering, and it determines whether every velocity feature is off by one and whether every
distinct-count is over the wrong window.

---

## 6 — Decision engine

### F-42 · S1 · The degraded-mode velocity cap blocks customers who would not be blocked healthy — violating the document's own Golden Rule 3

`ARCHITECTURE §14`:

```python
DEGRADED_CAPS = {"txn_5m": 3, "txn_1h": 10, "amt_1h": 25_000_00}
```
`ARCHITECTURE §09`:
```python
if ctx.payer.velocity_1h > settings.rail_velocity_1h:
    return block("VELOCITY_CAP")
```
`ARCHITECTURE §20.8`, Golden Rule 3: *"No degradation path produces a block that would not occur
healthy."* Property-tested per `§18`: `test_degrades_to_friction_never_block`.

A healthy `rail_velocity_1h` is necessarily well above 10/hour (a small merchant does more than
that before lunch). During a Redis blip, the cap drops to 10 and **customers are blocked who would
have been allowed with all systems healthy.** That is a direct, explicit violation of the golden
rule, the P5 principle, and the named property test — stated on page 14 and contradicted on page 9
of the same document.

The document arrives here while congratulating itself for having found this exact class of bug:
*"A hole this table had, found by Satyum's fail-closed principle."*

Compounding it: the counter is **per-worker, in-process**. With N workers the effective cap is
`N × 10/hour`, which customer hits it is non-deterministic, and it resets on every deploy. It is
neither a security control (bypassable by retry) nor a safe one (blocks legitimate users).

### F-43 · S1 · The NPCI cooling rail uses the wrong quantity and cannot be computed correctly

```python
if ctx.payee.age_hours < 24 and amount > 5_000:
    return cap(5_000, "NPCI_NEW_PAYEE_COOLING")
```

Two independent errors:

1. **Wrong semantics.** The NPCI cooling period applies for 24 hours after **this payer adds this
   beneficiary**. It is a property of the `(payer, payee)` relationship, not of the payee account.
   The code checks payee *account age*. Consequences in both directions: a 5-year-old account newly
   added by this payer — the overwhelmingly common real case, and the exact case the rule exists to
   protect — **is not capped**; a genuinely new merchant account receiving from established
   customers **is** capped, for no reason.

2. **The quantity is not knowable.** `f:payee:{acct}:first_seen` records when *this bank instance*
   first saw the account, not when it was opened. For an inter-bank payee — most of them — those
   differ by years. So `payee_age_days` is systematically wrong, and the Act 1 interstitial line
   **"This account was created 3 days ago"** is a claim the system cannot support. What it can
   support is *"we first saw this account 3 days ago"*, which is a materially weaker statement and
   is what an analyst or a regulator would hold you to. There is no standard rail-level API that
   returns beneficiary account-open date; this is a genuine capability gap presented as a feature.

Also: `24` and `5_000` are hardcoded literals in a system whose entire governance story is that
policy is versioned and stamped on every decision.

### F-44 · S1 · `CAP` is not on the friction ladder, so `attach_advisory` throws on the cooling path

```python
FRICTION_LADDER = [ALLOW, ALLOW_MONITOR, STEP_UP, STEP_UP_INTERSTITIAL, HOLD]
...
idx = min(FRICTION_LADDER.index(d.action) + steps, ...)
```

`§09` returns `cap(5_000, "NPCI_NEW_PAYEE_COOLING")`. `CAP` appears in the Postgres `action` enum
(`ALLOW|MONITOR|STEP_UP|HOLD|BLOCK|CAP`). It is **not in `FRICTION_LADDER`**.

`list.index()` raises `ValueError` on a missing element. So **every new-payee-cooling transaction
that also carries any advisory throws an unhandled exception on the hot path.** That is the
intersection of the two most common paths in the APP-scam demo: a new payee (cooling rail) and a
consortium advisory. It is the Act 1 transaction.

### F-45 · S2 · The trusted-pair fast path bypasses the ring rail

```python
if (ctx.pair.txn_count_90d >= 5
        and ctx.pair.last_disposition is not FRAUD
        and amount <= ctx.pair.p95_amount * 1.5
        and not ctx.device.is_new
        and not ctx.payee.in_any_blocklist):
    return allow(reason="TRUSTED_PAIR", scored=False)
```

It checks blocklists. It does **not** check `graph.ring_confirmed`, which `§09` treats as a hard
rail two lines later. A payee inside a confirmed mule ring is waved through for any payer with five
prior payments to it. Given F-26 (merchants are rings), this inconsistency cuts both ways and is
pure accident either way.

### F-46 · S2 · The fast path's safety argument is false for two live typologies, and it uses `is not` on a value

> *"the money in an abused trusted pair goes to someone the victim genuinely knows, which is the one
> place a fraudster gains nothing."*

Counterexamples that are among the most common frauds in the world:

- **Compromised known payee.** Mule accounts are frequently taken-over real accounts. The victim
  genuinely knows them; the account is no longer theirs.
- **Invoice redirection / BEC.** The counterparty is genuinely known; the *account details*
  changed. This is the single largest category of high-value APP fraud by value in most markets.
- **VPA repointing.** The pair is keyed on `creditor_account`, but the payer's UPI experience is
  keyed on VPA, and a VPA can be repointed to a different account. Both the keying mismatch and the
  repoint are exploitable.

Separately: `ctx.pair.last_disposition is not FRAUD` uses **identity comparison** on what is
presumably a value loaded from a store. It works by accident for enum singletons and interned
strings and fails silently otherwise — in the *safety condition* of the largest bypass in the
system.

And `last_disposition` is Nazar's own prior decision used as a decision input, which is the thing
P7 and Golden Rule 5 forbid. The escape ("it is not a *model* feature") is technically true and
substantively the same failure: pairs dispositioned clean become permanently cheaper to attack.

### F-47 · S2 · The friction headline depends entirely on an uncited traffic assumption

> *"Roughly 70–80% of retail payment traffic is a customer paying someone they have paid many times
> before"* … *"allowlisting the 75% that is obviously fine is what makes 1.8% achievable at all."*

No citation. And the document states the dependency explicitly, so `challenge_rate = 1.8%` — the
number in the closing line of the pitch — is a direct function of an unsourced assumption.

### F-48 · S2 · The engine minimises expected *loss*, not expected *cost*, contradicting the playbook's own cost curve

`PLAYBOOK §08` gives the correct objective:

```
total_cost(θ) = fraud_value_missed(θ) + challenge_rate(θ) × abandonment × margin
              + false_blocks(θ) × cost_per_complaint
```

`ARCHITECTURE §09` then implements a fixed ladder on `expected_loss` alone. Friction cost appears
nowhere in the decision rule. Consequence: a ₹5,000 EL triggers the same action for a customer with
a 95% step-up pass rate and one with a 40% pass rate, though the expected cost differs by more than
the EL does. `§21.5` then says the operating point should be segmented by
`tenure_band × rail × amount_band` — which is a **third**, incompatible specification of the same
decision rule. Three sections, three objectives, no reconciliation.

### F-49 · S2 · There is no timeout action, which is the most important line in a payments risk engine

Neither document states what happens when Nazar exceeds its deadline or is unreachable **from the
caller's perspective**. A UPI transaction has an NPCI-imposed end-to-end timeout; the bank's switch
makes an inline advice call with a hard deadline and a **default action on timeout**.

That default — fail-open (approve) or fail-closed (decline) — *is* the system's real security
posture, and it belongs to the bank, not to Nazar. It is not mentioned once. The entire design is
written as though Nazar owns the payment.

### F-50 · S1 · Act 1's demo contradicts itself and ships a dark pattern

`PLAYBOOK §12`:

> *"The payment does **not** get blocked. That would be arrogant and wrong. It gets an
> interstitial…"*

Eight lines later:

> *"Let them tap 'I'm sure.' Expected loss clears the hard rail — it blocks anyway."*

So it does get blocked, and the "I'm sure" button does nothing. **A consent affordance that has no
effect is a dark pattern**, and it will be named as one by any judge from a bank or a regulator.
The UK Confirmation-of-Payee / PSR regime the playbook cites as precedent has genuinely overridable
warnings — that is the entire design of the control.

Also, "expected loss clears the hard rail" is incoherent: a hard rail is by the document's own
definition **score-independent** (*"absolute, score-independent, checked first"*). If it fires on
expected loss, it is not a rail; it is a threshold, and the two-stage story is a threshold with a
warning screen in front of it.

---

## 7 — Consortium and trust

### F-51 · S1 · "Non-invertible, enumeration-resistant" is false, and it is the most technically wrong security claim in the documents

```json
"token": "9f3a…c2e1",   // HMAC-SHA256(pepper, "creditor_account:501001234")
```
> *"The pepper is held by consortium members and never by the registry operator, so the operator
> sees non-invertible, enumeration-resistant tokens."*

The claim is true for the **registry operator** and **false for every member**, and the document
does not distinguish them.

Indian bank account numbers are structured and low-entropy: typically 9–16 digits, IFSC-scoped,
issued near-sequentially within a branch. **Any member holding the shared pepper can enumerate a
target bank's account range in minutes** and invert the entire registry.

Therefore: Bank A can recover Bank B's complete confirmed-fraud customer list. That is the precise
outcome the DPDP argument in `PLAYBOOK §15F` exists to prevent — *"Canara will not hand HDFC its
customer records"* — and the design hands them over, one HMAC evaluation at a time.

`ARCHITECTURE §16`'s "naming discipline" section is correct and admirable about *not saying PSI* —
and then draws the wrong security conclusion from it. Being honest that it is not PSI does not make
a shared-pepper HMAC over a small domain into a confidentiality control. It is a **pseudonymisation
control**, which is a real and useful thing, and must be described as one.

Aggravating factors:

- **One shared pepper.** A single compromised member deanonymises the entire registry,
  retroactively and permanently.
- **No rotation.** Rotating the pepper invalidates every token ever published. The wire format has
  `"v": 1` for the protocol only — no pepper epoch, no key ID.
- **No per-pair or per-epoch derivation.**

**Required:** either state the threat model honestly (pseudonymisation + contractual controls,
which is what credit bureaus actually do), or implement a **2HashDH OPRF** so no member can compute
tokens offline — roughly 150 lines and one EC operation (~50 µs) with a curve25519 library. If you
intend to claim privacy, claim it with the second. See `docs/05-GRAPH-CONSORTIUM.md §4`.

### F-52 · S1 · The consortium blocklist has no revocation, expiry, or dispute path

The wire protocol defines `"op": "report"`. That is the only operation.

There is no `retract`, no `dispute`, no TTL, no decay, no expiry on any entry. Once two members
report a token, **that payee is blocked at every participating institution, permanently, with no
mechanism to undo it.** `ARCHITECTURE §21.7` waves at grievance redressal as out of scope — but
grievance redressal is precisely the mechanism that makes a shared blocklist legally operable. A
national blocklist with no off-switch is a customer-harm machine.

### F-53 · S1 · The `>= 2 reporters` block rail has no independence enforcement and no reputation gate in the code

`ARCHITECTURE §16` says reputation *"gates whether a report counts toward the `>= 2 reporters` rail
in §09."*
`ARCHITECTURE §09` reads a raw integer:

```python
if ctx.payee.consortium_reporters >= 2:
    return block("PAYEE_MULTI_BANK_REPORTED")
```

No reputation term. And "independent" is asserted, never enforced: nothing prevents two reports from
the same legal entity under two participant codes, two BINs, or two subsidiaries; nothing detects
collusion; nothing detects one member reporting the same token twice through two keys.

This is a **hard block, on a foreign claim, in a system whose central marketing line is that a
foreign institution cannot block your customer.** The documents draw the safety line at 2 and
supply no mechanism that makes 2 mean anything.

### F-54 · S1 · A Bloom filter on a blocking path blocks innocent people by design

`PLAYBOOK §10`: *"distributed as a Bloom filter"*, *"Build the Bloom filter first — it works
standalone and demos in thirty seconds."*

A Bloom filter has false positives by construction. On a **BLOCK** path, a false positive blocks a
randomly chosen innocent payee, with a reason code that says another bank reported them. At a 1%
FPR and any real payee volume, that is a steady stream of wrongful blocks with a confident,
false, cross-institutional explanation attached.

The FPR is not mentioned. The consequence is not mentioned.

**Required:** Bloom/cuckoo filter as a **negative cache only** — a miss means "definitely not
listed, skip the round trip"; a hit means "maybe, go confirm exactly." Never authoritative.

### F-55 · S2 · Three incompatible consortium mechanisms are described and never reconciled

- `PLAYBOOK §10`: "signed append-only log, distributed as a Bloom filter"
- `PLAYBOOK §10`: "where blockchain belongs" — a distributed ledger with no single owner
- `ARCHITECTURE §16`: a `prev_hash`/`hash` chain over entries

A hash chain requires a **total order**. Who assigns it across mutually distrusting institutions?
If the registry operator does, then there *is* a single trusted operator and the "no single owner"
pitch is false. If nobody does, the chain cannot be constructed. No consensus mechanism is
specified. This is the load-bearing structural question of the layer and it is unanswered across
both documents.

**Required:** per-reporter chains (each member chains only its own entries — no global order, no
consensus, no trusted operator needed), plus a registry-published periodic Merkle root over all
reporter heads for cross-checking. See `docs/05-GRAPH-CONSORTIUM.md §4.4`.

---

## 8 — Data model

### F-56 · S1 · The `labels` DDL is invalid Postgres and will not create

```sql
matured BOOLEAN GENERATED ALWAYS AS (available_at <= now()) STORED
```

A `STORED` generated column's expression must be **`IMMUTABLE`**. `now()` is `STABLE`.
Postgres rejects this: `ERROR: generation expression is not immutable`.

Copy-paste fails on the first migration.

### F-57 · S2 · The maturity guard does not actually prevent the leak it is named for

> *"Training queries must filter `WHERE matured`, which is a one-line guard against the most
> seductive leak in fraud modelling: training on labels that had not arrived yet at the moment you
> are pretending to score."*

`available_at <= now()` filters on *training* time. To prevent the named leak you need
`available_at <= <as-of timestamp of the simulated decision>` — i.e. a point-in-time join per row,
not a global boolean. The column as designed prevents training on labels that have not arrived
**today**; the leak is training on labels that had not arrived **at the decision timestamp of each
row**. Different guard, different query, different result.

### F-58 · S1 · `CREATE UNIQUE INDEX ON decisions (end_to_end_id)` makes three named features impossible

One decision row per transaction, forever. That forbids:

- **Degraded-window replay** (`§14`: *"the entire degraded window is replayed through full scoring
  once the store recovers"*) — the replay produces a second decision for the same id.
- **Shadow mode** (`PLAYBOOK §10`: *"upi-v2 scores everything, decides nothing"*) — where do those
  rows go?
- **Step-up resolution** — the decision changes when the challenge is passed or abandoned.
- **Policy A/B re-scoring** and any post-hoc re-evaluation.

**Required:** `PRIMARY KEY (end_to_end_id, decision_seq)` with a `decision_kind`
(`LIVE | SHADOW | REPLAY | RESOLUTION`) discriminator.

### F-59 · S1 · The hash chain is a global serialisation point, incompatible with the horizontal-scaling claim

```
hash = sha256(prev_hash || canonical_json(row))
```

Computing `prev_hash` requires a **total order over all decisions**. `§13` claims *"this design
scales horizontally because scoring is stateless."* You cannot have N stateless writers and a single
chain without a global lock or a single writer — which is then the throughput ceiling of the whole
system.

Compounded by `§14`'s *"Postgres write fails → queue and retry"*: retried writes arrive **out of
order**, which breaks the chain irreparably. And by F-05: a fire-and-forget write that is lost
leaves a **hole in a tamper-evident log**, which is strictly worse than no log, because the log's
whole value is that gaps are detectable and here gaps are routine.

**Required:** per-shard chains + periodic cross-shard Merkle anchoring, and a durable local
write-ahead append **before** the response is returned. See `docs/01-LATENCY-RESILIENCE.md §6`.

### F-60 · S2 · There is no settlement or outcome state, so `value_recall` is uncomputable

The schema has `transactions` and `decisions`. It has **no record of what happened**: did the
payment settle, was it recalled, was the step-up completed or abandoned, was money recovered.

Consequences:

- `value_recall` (*"% of fraud value stopped"*, on the wall in `PLAYBOOK §08`) cannot be computed.
- `step_up_pass_rate` cannot be computed.
- `§21.2`'s entire step-up-outcome-as-control-input design has nowhere to write.
- `§21.3` bolts `recovered_minor` onto `cases`, but recovery is a property of a **transaction**, not
  of a case; a case covering 40 transactions cannot carry one recovery amount.

### F-61 · S2 · At-least-once delivery meets a `PRIMARY KEY` and an infinite retry loop

Redis Streams consumer groups are **at-least-once**. A redelivered event re-scores and re-writes.
`end_to_end_id TEXT PRIMARY KEY` rejects the duplicate; `§14` says *"queue and retry"*; the retry
fails identically, forever.

No `ON CONFLICT`, no consumer-side dedupe, no idempotency key discipline. (Credit where due: the
Redis zsets *are* accidentally idempotent, because `ZADD` with `member = end_to_end_id` overwrites —
that is a genuine and probably unintentional strength of the design.)

### F-62 · S3 · `bank_instance TEXT NOT NULL -- 'A' | 'B' for the consortium demo`

A demo artefact hardcoded into the system of record. It should be a tenant/participant identifier
with a real registry behind it.

### F-63 · S2 · `narrative TEXT` on `cases`, generated once at open

Cases grow — a ring accumulates alerts and accounts over hours. A narrative frozen at open time is
stale immediately, and the analyst reads a description of a smaller, earlier case. The schema
decision was driven by a demo line (*"it was written before they clicked"*) rather than by the
workflow.

### F-64 · S3 · `amount_minor BIGINT ... never a float`, and then amounts go into a double-scored zset

`§11` is emphatic (*"A rounding error visible on screen in a payments demo is unrecoverable"*), and
`§05`'s `w:payer:{acct}:amt` puts amounts into Redis sorted-set scores, which are IEEE-754 doubles.
Exact up to 2⁵³ paise (≈ ₹90 trillion) so it does not bite in practice — but it is the document
violating its own stated invariant, and it will bite if anyone stores a running sum.

---

## 9 — Resilience and operations

### F-65 · S2 · "Abandon model inference at 20ms" is not implementable in the specified design

`§14`: *"Model inference exceeds 20 ms → Abandon, rules-only for that transaction."*
`§13`: *"GBM inference … `num_threads=1` — thread pools cost more than they save at n=1."*

LightGBM `predict` is a **blocking foreign call**. You cannot cancel it. Abandoning it requires
running it in an executor and dropping the future — i.e. a thread pool, which `§13` explicitly
rejects two pages earlier.

And abandoning does not help: the CPU is consumed regardless. Under overload, abandoning work you
have already paid for while accepting more work is the classic path to queueing collapse.

### F-66 · S2 · No overload behaviour at all — no admission control, no load shedding, no circuit breakers with thresholds

`§14` covers **dependency failures**. It does not cover **overload**, which is where p99 actually
dies. There is no answer to "what happens at 3× expected TPS", no concurrency limiter, no queue
bound, no shed policy, no defined circuit-breaker thresholds or half-open behaviour.

### F-67 · S2 · Redis is a single point of failure with no HA design

No Sentinel, no Cluster topology, no replica strategy, no failover time budget, no read-from-replica
staleness discussion. "Redis unreachable" is one table row whose answer is a per-worker in-process
counter (F-42).

### F-68 · S2 · WebSocket fan-out shares the process that must hold the p99

`PLAYBOOK §05` chooses native WebSocket from FastAPI **specifically to own the latency number**, and
thereby places broadcast fan-out on the exact resource the latency number depends on. At 5,000 TPS
× 10 connected consoles that is 50,000 JSON serialisations per second on the scoring event loop.

Also: no console can render 5,000 rows/sec. There is no sampling, aggregation, or coalescing design
— the UI transport is specified as a firehose.

### F-69 · S2 · Python/asyncio for a p99-constrained hot path, while invoking national scale throughout

Defensible for a prototype. Not defensible alongside "738 million times a day" and "p99 < 50ms
under load." The documents never state which they are optimising for, and repeatedly borrow the
production framing's credibility while making prototype choices. **This is the core rhetorical
problem with both documents, and it is what a hostile judge will find first.**

### F-70 · S3 · Every test in `§18` is a demo beat

The test table is genuinely good and the "demo beats are tests" idea is excellent. But there are:
no load tests, no chaos tests beyond "kill Redis", no property tests over the Redis window
arithmetic (where F-32/F-33 live), no golden-file tests for feature computation, no backtest
harness, no calibration regression test, no test that the feature catalogue and the key layout
agree (which would have caught F-34 immediately).

---

## 10 — Security and compliance

### F-71 · S1 · No authentication or authorisation on any endpoint

`ARCHITECTURE §12` lists the full API. Every one of these is unauthenticated as specified:

| Endpoint | What it lets an unauthenticated caller do |
|---|---|
| `PUT /v1/policy` | **Change the risk appetite for every customer of the bank** |
| `POST /v1/rules/reload` | Load arbitrary rule definitions into the decision path |
| `POST /v1/redteam/fire` | Inject synthetic transactions into the live stream |
| `POST /v1/cases/{id}/disposition` | Confirm fraud → blocklist → consortium publish |
| `GET /v1/entities/{kind}/{id}` | Enumerate any customer's full behavioural profile |

No RBAC, no authn, no four-eyes on policy change, no audit of *who* changed a threshold — in a
system whose central credibility claim is a tamper-evident audit log. The audit log records
decisions and not the configuration changes that produced them.

### F-72 · S2 · The rules DSL is a potential RCE and its evaluation is unspecified

```yaml
when: payee.distinct_payers_1h >= 8 and payee.account_age_days < 30
```

`§13` says *"Pre-compiled predicates, not `eval`"* — correct instinct, no mechanism. If the
implementer reaches for `eval()` (the obvious way to make that YAML work), then
`POST /v1/rules/reload` — unauthenticated, per F-71 — is **remote code execution on the decisioning
service**.

**Required:** a restricted expression language with a documented grammar (CEL, or a hand-rolled
AST over a whitelisted operator/identifier set), no attribute traversal, no function calls.

### F-73 · S2 · The database stores everything the consortium design refuses to send, unencrypted, forever

The privacy argument in `PLAYBOOK §15F` / `ARCHITECTURE §16` is that customer data cannot leave the
perimeter, so only a token crosses. Inside the perimeter, `§11` stores `debtor_account`,
`creditor_account`, `creditor_vpa`, `device_id`, `ip INET`, `geo_cell` in cleartext, with:

- no encryption at rest specified
- no column-level tokenisation for the analytics/training path
- no retention policy
- no DPDP data-subject-rights procedure (access, correction, erasure)
- no data classification

A judge from a bank's compliance function will ask about the database before they ask about the
wire.

### F-74 · S2 · The system ships a free fraud-reconnaissance oracle

Every payment returns a friction level. A fraudster sends ₹1 to a mule and reads the response to
learn whether the mule is burned — before committing the real money. No rate limiting, no response
obfuscation, no anomaly detection on probing patterns, no mention of the problem.

This is a *product feature* of the design as specified, and it directly undermines the consortium
layer: the moment a token is published, the attacker can detect it for ₹1 and rotate.

---

## 11 — Claims that will not survive Q&A

### F-75 · S3 · The UPI arithmetic is internally inconsistent and understates the requirement

> *"23.2 billion transactions … in May 2026 — about 738 million a day, averaging above 5,000 TPS"*

`23.2e9 / 31 = 748.4M/day`, not 738M. `748.4e6 / 86400 = 8,662 TPS` average — the doc says "above
5,000", which is true and understates by ~70%. With a realistic 3–5× peak-to-average ratio, peak is
**25,000–40,000 TPS**, which is a materially harder problem than the document frames and makes the
single-Redis, single-hash-chain, Python-asyncio design substantially less defensible.

Understating your own scale requirement is an odd error to make in a section whose purpose is to
justify the design by invoking scale.

### F-76 · S3 · Every 2025–2026 regulatory and volume claim rests on secondary sources and needs re-verification

I have not independently verified these and you must before anyone says them aloud:

| Claim | Source cited | Problem |
|---|---|---|
| Authentication Directions in force 1 Apr 2026, "explicitly sanction risk-based authentication" | two law-firm notes + a vendor blog | **The single most load-bearing claim in the pitch, and the RBI's own circular is not cited.** Quote the Directions directly or the framing is hearsay |
| DPIP "still under development, no launch date" as of "early 2026" | news aggregators | It is **August 2026**. "Early 2026" is 7+ months stale. If DPIP has launched, `§00`'s entire positioning inverts from opportunity to liability |
| UPI May 2026 volumes | news aggregator, not NPCI | Cite NPCI's own statistics page |
| MuleHunter.AI at 23 banks (RTI, Dec 2025) | Medianama | Plausible; verify currency |

### F-77 · S3 · Quoting "~95% accuracy" while spending a page explaining that accuracy is a worthless metric

`PLAYBOOK §01`: *"Canara reported ~95% accuracy."*
`PLAYBOOK §13`: *"accuracy and ROC-AUC both look artificially good on imbalanced fraud data."*

On a mule-detection task with a sub-1% base rate, 95% accuracy is consistent with detecting nothing.
Quoting it approvingly, from a press report, in a deck that condemns the metric, hands a judge a
free shot.

### F-78 · S3 · "Scoring is stateless and horizontally scalable" is true and misleading

Scoring is stateless. The **system** is not, and every hard part is in the stateful half: the Redis
profile keyspace (F-35, F-38), the `(payer, payee)` cross-product, the graph (F-28), and the hash
chain (F-59). Answering a scaling question with "scoring is stateless" is answering the easy half
out loud.

### F-79 · S3 · "Roughly 700 lines of Satyum ports" and "collapses #12 from a day of work to a port"

The named files sum to 488 LOC. And the port is not a port: entity kinds change, exact-match
replaces Hamming-distance matching, the graph weight table **means something different in this
domain** (F-26), and the wire protocol needs signature verification, revocation (F-52), reputation
enforcement (F-53), and an independence model (F-53) that a document-integrity federation almost
certainly does not have. Moving it earlier in the build order on the grounds that it is "just a
port" is a scheduling claim that will not survive first contact.

### F-80 · S3 · The self-audit is a rhetorical device, not a control

`ARCHITECTURE §20`: *"Ten design decisions reviewed. Three found live bugs in this spec."*

Every finding in this review survived that review, including: a feature computed over the wrong
window (F-32), a storage design that cannot compute three of its own features (F-33, F-34), DDL that
does not create (F-56), an exception on the demo's own transaction (F-44), a golden rule violated
by the section that congratulates itself for finding that class of bug (F-42), and a hard BLOCK on
"three people paid the same merchant" (F-26).

A self-audit that finds three bugs and misses forty is worse than none, because it is presented as
assurance.

### F-81 · S3 · Speaking in the production voice invites the question that kills you

> *"Nazar decides 700 million times a day, in 38ms."*

Nazar decides nothing 700 million times a day. This sentence, and the dozen like it, is what
converts a strong prototype into an overclaim. The fix costs nothing: state the deployment profile
every time a number appears.

---

## 12 — Stubs, undefined functions, and hardcoded checks

Consolidated list of things that are named but do not exist, or exist as literals where the
document's own governance story requires configuration.

**Undefined functions on the decision path**

| Symbol | Where | Consequence |
|---|---|---|
| `squash(novelty_z)` | `§08` | Produces the advisory `suspicion` value |
| `_escalation(suspicion)` | `§10` | Determines how many friction rungs a customer climbs |
| `contradicts_action(narrative, action)` | `§20.5` | The entire LLM guardrail. NLP-hard as stated |
| `token(account)` | `§05` pipeline | Pepper source, normalisation, and epoch all unspecified |
| `allow() / cap() / block() / step_up()` | `§09` | Return-type contract never defined |
| `ProfileBundle.from_pipeline` | `§05` | Positional parse of 28 results (F-37) |
| `d.with_annotations()` vs `d.model_copy()` | `§10` | Dataclass and Pydantic idioms mixed in one function — the code was never run |

**Hardcoded values that the governance story says must be policy**

| Value | Where | Should be |
|---|---|---|
| `50 / 500 / 5_000` EL ladder | `§09` | Versioned policy, per segment (`§21.5`) |
| `5_000` and `24` in the cooling rail | `§09` | Regulatory parameter with an effective-date |
| `LOSS_GIVEN_FRAUD` dict literal | `§09` | Per-rail parameter calibrated from recovery data |
| `DEGRADED_CAPS` literal | `§14` | Policy, and see F-42 |
| `FLOOR = 0.02`, `Z_PLAUSIBLE = 25.0` | `§06` | Per-feature config; `25.0` is asserted with a story, not derived |
| `min_ring_size = 3`, `ring_weight_threshold = 1.0` | `§07` | See F-26 |
| `NOVELTY_HIGH`, `MODEL_LOW` | `§08` | Never given values or a derivation |
| `challenger_pct`, `crc32(...) % 100` | `§21.4` | Fine, but no ramp/kill mechanism |

**Named but unspecified subsystems**

- The **15 rules** — `PLAYBOOK`'s own gaps section admits *"specified as a format, not written."*
  Every fitted-weight claim, every double-counting answer, and the whole composition layer depends
  on rules that do not exist.
- **Reporter reputation** — formula given (`confirmed/(confirmed+dismissed)`, "decayed"), decay
  unspecified, and not referenced by the code that is supposed to use it (F-53).
- **Feature integrity checker** (`§20.2`) — the SQL (`FANIN_1H_SQL`) that constitutes the second
  derivation is the hard part and is not written. Given F-32, the streaming derivation is wrong, so
  this checker would fire on 100% of samples on day one — which is actually the best thing in the
  document, if it existed.
- **The entire UI** — `PLAYBOOK` names it *"the largest remaining gap"* while also naming
  presentation as the top priority.

---

## 13 — What is actually good

A hostile review that finds nothing to keep is not a review. These are genuinely strong and are
carried forward into the rebuilt documents:

1. **P6 — mandatory explanation enforced at construction.** Four lines, structural, correct. Keep
   verbatim.
2. **P8 — four-state signal status (`FIRED / CLEAR / NOT_APPLICABLE / NOT_EVALUATED`).** The
   observation that a device check that never ran must never render as a device check that passed is
   the best single idea in either document. Keep verbatim.
3. **P7 — no feature derived from the system's own prior decisions.** Correct and correctly
   motivated; it just needs the data-side complement (F-12).
4. **Rail-specific `LOSS_GIVEN_FRAUD`.** Recognising that irreversibility is the dominant term is a
   genuinely payments-native insight and most teams miss it entirely.
5. **Denominator floors / the MAD=0 analysis.** Correct, well-motivated, and the
   *"off by orders of magnitude is a bug; off by a factor of six is a fraud"* discrimination is a
   good operating principle.
6. **Label maturity as a first-class concept.** Right instinct, broken implementation (F-56, F-57),
   worth fixing rather than dropping.
7. **The dual-derivation feature integrity check (`§20.2`).** The single best idea in the
   architecture. Build it — it would have caught F-32 on day one.
8. **LLM prompt-injection via the UPI remittance field.** A real, specific, payments-native attack
   most teams would never consider, with correct mitigations (structured input only, contradiction
   firewall, must-fail fixture).
9. **Refusing a hosted LLM API on DPDP grounds.** Correct, and the reasoning generalises.
10. **The exclusion table (`§20.9`).** Naming what you deliberately did not build, with reasons, is
    the strongest available credibility signal and almost nobody does it.
11. **"Absence of evidence is not evidence of absence" — never render an allowed payment as
    "verified."** Correct and unusually disciplined.
12. **Showing the failure case (slow drip).** The instinct is exactly right, even though the number
    itself is generator-determined (F-06).
13. **Demo beats as named tests.** Excellent practice; it just needs the other 80% of a test suite
    underneath it (F-70).

---

## Where the rebuilt design lives

| Finding cluster | Resolved in |
|---|---|
| Measurement, latency, overload, HA, audit chain, WAL | [docs/01-LATENCY-RESILIENCE.md](docs/01-LATENCY-RESILIENCE.md) |
| Profile store, keys, features, point-in-time, DDL | [docs/02-DATA-AND-FEATURES.md](docs/02-DATA-AND-FEATURES.md) |
| Training data, calibration, rules, novelty, attribution, feedback, adversarial | [docs/03-ML-PIPELINE.md](docs/03-ML-PIPELINE.md) |
| Decision rule, rails, fast path, advisory boundary, degraded mode | [docs/04-DECISION-POLICY.md](docs/04-DECISION-POLICY.md) |
| Graph, ring weights, consortium, tokens, revocation | [docs/05-GRAPH-CONSORTIUM.md](docs/05-GRAPH-CONSORTIUM.md) |
| Stack, topology, service split, trust boundaries | [docs/00-ARCHITECTURE.md](docs/00-ARCHITECTURE.md) |
| Build order, acceptance gates, claims register | [docs/06-BUILD-PLAN.md](docs/06-BUILD-PLAN.md) |
