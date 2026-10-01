# 04 — Decision Engine and Policy

**Fixes:** F-20 F-21 F-42 F-43 F-44 F-45 F-46 F-47 F-48 F-50

Kept short. The architecture here is four ordered stages and one objective function; everything else is a config table.

---

## 1 — The decision, in order

```
  1. LOCAL FILTERS + REGULATORY RAILS      zero I/O, ~5 µs, absolute
  2. TRUSTED-PAIR FAST PATH                (gated — §4)
  3. SCORE                                 p = calibrated, prevalence-corrected
  4. EXPECTED-COST MINIMISATION            → base action
  5. POLICY RAILS                          may raise friction; may never BLOCK
  6. ADVISORY ATTACHMENT                   monotone, capped below HOLD
  → Decision
```

Stage 1 is computable with no network call, which is what makes the deadline guarantee in
[01-LATENCY §2](01-LATENCY-RESILIENCE.md#2--the-deadline-and-the-timeout-action) real: under any
failure, the system still returns a rails-only decision rather than an error.

---

## 2 — The objective: expected *cost*, not expected loss

The previous design implemented a fixed ladder on expected loss while its own playbook stated the
correct objective and a third section specified per-segment operating points — three incompatible
specifications (F-48). One objective, here:

```
  action* = argmin over actions of:
              P(fraud) · amount · LGF[rail] · (1 − P(stop | action))     ← fraud loss
            + friction_cost[action]                                       ← customer cost
            + (1 − P(fraud)) · P(abandon | action) · margin               ← lost good business
```

| Term | Source |
|---|---|
| `P(fraud)` | [03-ML §4.3](03-ML-PIPELINE.md#43--prevalence-correction), prevalence-corrected |
| `LGF[rail]` | Policy bundle. **The most payments-native parameter in the system** — kept from the previous design, which got this right |
| `P(stop \| action)` | Measured from `transaction_outcomes`: step-up pass rates, interstitial cancel rates, hold outcomes |
| `friction_cost[action]` | Policy bundle, per segment |
| `P(abandon \| action)` | Measured per segment from `transaction_outcomes` |

Every input is either a policy parameter or a measured quantity from a table that now exists
([02-DATA §7](02-DATA-AND-FEATURES.md#7--postgres-schema)) — the previous schema had no
`transaction_outcomes`, so three of these four were uncomputable (F-60).

### 2.1 — Why irreversibility dominates

| Rail | Reversible? | Recovery path | `LGF` |
|---|---|---|---|
| UPI / IMPS | No, once settled | Beneficiary-bank freeze — usually too late | ~0.95 |
| NEFT | Within the batch window | Recall before settlement | ~0.5 |
| Card CNP | Yes | Void, then chargeback with liability shift | ~0.2 |

The consequence is concrete and worth saying out loud: **the step-up threshold on a push rail sits
far lower than on a card rail, because a miss on UPI is a near-total loss and a false challenge
costs thirty seconds.** This is also why APP fraud dominates in India specifically — the rail
carrying most volume is the one where money cannot be clawed back. Kept verbatim from the previous
design; it was the best payments insight in it.

`LGF` values are **calibrated from your own recovery data**, not quoted. State that.

### 2.2 — Segmentation

One operating point for everyone is wrong. `tenure_band × rail × amount_band`, each cell with its
own target challenge rate drawn from the same cost curve. The console shows challenge rate per cell,
and a spike in one cell is a policy bug you can see. Kept from the previous design's `§21.5` —
correct idea, now actually reconciled with the decision rule instead of contradicting it.

---

## 3 — Rails

Two classes, never conflated — different authority, different change process, different customer
answer.

### 3.1 — Regulatory rails (absolute, may BLOCK or CAP)

```yaml
- id: RAIL-001
  class: regulatory
  authority: "NPCI UPI beneficiary cooling period"
  verified_on: "2026-08-14"                              # ← re-verify before every release
  when: pair.first_added_within_hours < 24 && amount_minor > 500000
  action: CAP
  cap_minor: 500000
```

**The cooling rail is fixed (F-43).** It keys on `(payer, payee)` beneficiary-addition time, not
payee account age. The previous version checked account age, which fails in the common case (an old
account newly added by this payer — exactly what the rule protects against) and fires spuriously on
new merchant accounts. And it applied to a quantity the system cannot know: `f:{b}:first_seen` is
when *we* first saw the account, not when it was opened.

**Consequence for the UI (D8):** the interstitial says *"we first saw this account 3 days ago"*, not
*"this account was created 3 days ago."* The second is a claim the system cannot support for any
inter-bank payee, and it is the kind of claim a regulator holds you to.

### 3.2 — Policy rails (ours, may never BLOCK)

```yaml
- id: RAIL-101
  class: policy
  when: payer.txn_count_1h > payer.baseline_txn_1h_p999 * 3
  action: STEP_UP_INTERSTITIAL       # NOT block
```

Note the predicate: **relative to the payer's own baseline**, not an absolute count. The previous
design used an absolute velocity cap wired to `BLOCK` — an absolute threshold in a system whose
first principle forbids them, and a customer-blocking outcome from a policy control (F-42, D3, D7).

### 3.3 — Local blocklist

The only non-regulatory path to `BLOCK`, and only after: exact confirmation against a sharded key
(never a filter hit — F-54), and an analyst disposition with four-eyes approval. Never from an
algorithm's output alone.

---

## 4 — Trusted-pair fast path

The previous design called this *"the single largest lever on both latency and friction"* and it is
— but it read three fields that had no storage design (F-34), skipped the ring rail (F-45), used
`is not` for a value comparison (F-46), and rested its safety argument on a claim that is false for
two of the most common high-value frauds in the world.

```go
func trustedPair(ctx *ScoringContext) bool {
    p, ev := ctx.Pair, ctx.Event
    return p != nil &&
        p.TxnCount90d >= 5 &&
        p.LastDisposition != DispositionFraud &&           // value compare, not identity
        ev.AmountMinor <= p.AmtP95Minor*3/2 &&
        !ctx.Device.IsNewToPayer &&                        // ATO cannot ride the fast path
        !ctx.Payee.InAnyBlocklist &&
        !ctx.Graph.RingFlagged &&                          // ← was missing (F-45)
        ev.CreditorAccount == p.LastCreditorAccount &&      // ← VPA repoint guard
        !ctx.Degraded.Any()                                 // ← never fast-path degraded
}
```

**The safety argument, corrected.** The previous claim — *"the money goes to someone the victim
genuinely knows, which is the one place a fraudster gains nothing"* — is false for:

- **Compromised known payee.** Mule accounts are frequently taken-over real accounts.
- **Invoice redirection / BEC.** The counterparty is genuinely known; the *account details* changed.
  This is the largest category of high-value APP fraud by value in most markets.
- **VPA repointing.** The pair keys on account; the payer's experience keys on VPA.

The `CreditorAccount == LastCreditorAccount` guard addresses the third directly. For the first two,
the honest answer is a **stated residual risk with a mitigation**: fast-pathed transactions are
**sampled at 2% into full scoring asynchronously**, and a fast-pathed pair whose payee later joins a
ring or a blocklist triggers a retro-review of that pair's recent traffic. Say the risk out loud;
it is real and bounded.

**P7 note:** `LastDisposition` *is* our own prior decision, which the golden rules forbid as a
feature. It is used here as a **decision input, not a model feature**, and that choice is recorded
in an ADR with its revisit trigger, rather than routed around silently as the previous design did.

**The 70–80% traffic figure is uncited (F-47), and the headline friction number depends on it
entirely.** At `[P0]` it is a property of the generator and must be labelled `[RECOVERED]`. At
`[P1]` it is measured. Never quote a challenge rate without stating the fast-path share it rests on.

---

## 5 — The advisory boundary

Kept from the previous design — the idea is genuinely good — with the two flaws fixed.

```go
var LADDER = []Action{ALLOW, ALLOW_MONITOR, STEP_UP, STEP_UP_INTERSTITIAL, HOLD}

func attachAdvisory(d Decision, adv []Finding, pol Policy) Decision {
    if d.Action == BLOCK || d.Action == CAP {          // CAP is off-ladder — fixes F-44
        return d.WithAnnotations(adv)
    }
    admissible := filterAdmissible(adv, pol)            // real checks — fixes F-21
    if len(admissible) == 0 {
        return d                                        // fail-open, byte-identical
    }
    idx := indexOf(LADDER, d.Action) + maxSteps(admissible, pol)
    idx = min(idx, indexOf(LADDER, pol.AdvisoryMaxRung))  // ← the cap. Fixes F-20
    return d.Escalate(LADDER[idx], admissible)
}
```

### The two fixes

**(a) `advisory_max_rung: STEP_UP_INTERSTITIAL`.** The previous ladder clamped to `len-1`, which is
`HOLD` — and a HOLD on an irreversible push rail is a block from the customer's point of view,
resolved in hours by an analyst queue, not three seconds (F-20). The flagship safety claim was false
by the ladder's own definition. With the cap, it is true:

> "A foreign advisory can add a step-up and an interstitial. It cannot reach hold, and it has no
> path to block — that's a cap in the policy bundle, property-tested, not a promise. Worst case a
> false report costs a customer one extra confirmation screen."

**(b) Admissibility is a real check.** The previous filter tested for a non-empty explanation, which
the type constructor already guarantees — dead code, and a "fail-open" invariant whose test passes
vacuously (F-21). Actual admissibility:

```go
func filterAdmissible(adv []Finding, pol Policy) []Finding {
    return filter(adv, func(a Finding) bool {
        return a.SignatureValid &&                                  // reporter signature verifies
               a.ReporterReputation >= pol.MinReporterReputation && // reputation floor
               a.Age <= pol.MaxAdvisoryAge &&                       // staleness bound
               a.Confidence >= pol.MinAdvisoryConfidence
    })
}
```

`deterministic_action` is preserved beside the final action, so the UI can say *"we would have said
STEP-UP on our own data; a foreign advisory raised it to STEP-UP + interstitial."* That was the best
UI idea in the previous design and it is kept.

---

## 6 — Degradation

**D7: degrade to more friction, never to a block that would not occur healthy, never silently to
allow.** Property-tested in CI over the cross product of every injected failure.

The previous design violated its own golden rule here: a degraded-mode velocity cap of 10/hour
blocks customers who would be allowed healthy (F-42). The fix is to **cap value, not deny**.

| Failure | Behaviour | Recorded |
|---|---|---|
| Redis slot down | Affected features `NOT_EVALUATED`; last-known-good profile with staleness stamp; **value cap** applied above a policy threshold; window queued for replay | `degraded=["profile:slot3"]` |
| Redis fully down | Rails-only + value cap. **Never a denial.** Full window replayed on recovery, cases tagged `DEGRADED_WINDOW` | `degraded=["profile_store"]` |
| Cold start | Features `NaN` (LightGBM handles natively), `COLD_START` finding, `cold_start_features_n` as an explicit feature | `findings[COLD_START]` |
| Model bundle bad | Rules + rails only; console shows the model lane dark; previous bundle auto-restored | `degraded=["model"]` |
| Graph down/stale | Graph features `NOT_EVALUATED` (not zero, not `NaN`); ring signals suppressed | `degraded=["graph"]` |
| Consortium down | **Fail-open** — no advisory. Byte-identical decision, by construction: advisories can only raise friction, so their absence cannot raise risk | `degraded=["consortium"]` |
| Postgres down | WAL absorbs; decisions continue; chain reconciles on drain | `degraded=["persist"]` |
| Overload | Admission control → rails-only, still a real decision, never an error | `degraded=["shed"]` |

**A value cap is not a denial.** Under degradation the customer can still send — up to a bounded
amount. That satisfies D7 where the previous design's transaction-count cap did not, and it bounds
loss during the window, which is the actual goal.

The degraded counter is per-worker and in-process, so **state the real bound**: with N workers the
effective cap is N× the per-worker cap. Don't claim a global guarantee an in-process counter cannot
give.

Demonstrating this deliberately — kill Redis mid-demo, watch it fall back with a visible banner,
bring it back, watch the window replay — is a stronger engineering signal than any green dashboard.
The previous design was right about that.

---

## 7 — Act 1, fixed

`PLAYBOOK §12` says *"The payment does not get blocked. That would be arrogant and wrong"* and then
blocks it eight lines later, after the judge taps "I'm sure" — a consent affordance with no effect
(F-50). Two honest versions; pick one and commit:

**Option A — the warning is real (recommended).** The interstitial is genuinely overridable. The
override is recorded, the case opens immediately at high priority, and the payment is held for
manual review rather than settled. This matches the UK CoP/PSR precedent the playbook cites as
justification — those warnings *are* overridable, and that is the design.

> "It doesn't block. It tells the customer what we know and lets them decide, because a bank that
> blocks a payment its customer insists on is a bank the customer leaves. What it *does* do is open
> a case the moment they override, and hold settlement pending review. The override is the strongest
> signal we get — nothing else tells you a human was warned and proceeded anyway."

**Option B — the rail fires first.** If the payee is on the consortium blocklist with ≥2 independent
reporters, that rail fires **before** the interstitial and the payment is blocked with no override
offered. Honest, but do not stage it as a choice the judge gets to make.

**What you may not do is Option B while presenting it as Option A.** Option A is the better demo
anyway: the judge's override *is* the beat, and the case opening on override is more interesting
than a block.

---

## 8 — Policy A/B and versioning

```go
bucket := crc32(payerAccount + policyEpoch) % 100      // stable per CUSTOMER, never per transaction
policy := ifThen(bucket < settings.ChallengerPct, POLICY_B, POLICY_A)
```

Stable per customer — a customer challenged on one payment and waved through on the next has had a
worse experience than either policy alone. Kept from the previous design; correct.

Every decision stamps `policy_version`, so the comparison is a `GROUP BY`, and combined with
`action_propensity` ([03-ML §7.2](03-ML-PIPELINE.md#72--data-path-new-and-the-harder-half)) a policy
change can be evaluated off-policy *before* it ships.

**The adoption answer:** at 1% of traffic, in shadow, with the cost curve as the readout.

---

## 9 — Step-up outcome as a control input

The previous design's `§21.2` is one of its best sections and is kept, now with a table to write to.

| Outcome | Meaning | Action |
|---|---|---|
| Completed < 10 s | Genuine customer, phone in hand | Lower friction on the pair for 24h; weak negative label |
| **Abandoned** | Could not complete the factor | **Strong-ish positive** (`confidence 0.3`). **Open a case even though nothing settled — the attempt is the evidence** |
| Completed > 60 s, multiple tries | Ambiguous | No adjustment; annotate the case |

**And the honest limit, which is also the APP argument:** for social engineering, step-up outcome
carries *no* signal — the victim completes instantly and confidently because they believe they are
supposed to. That is precisely why the Act 1 control is a **beneficiary warning** rather than another
authentication factor: it targets the victim's belief, not their identity. Authentication cannot fix
a problem where authentication succeeded.

---

## 10 — Recovery

Detection is half the product; a bank will ask about the money.

```
CONFIRM_FRAUD
  ├─ not yet settled   → recall / void            (NEFT batch window, card pre-capture)
  ├─ settled           → beneficiary-bank freeze request
  ├─ report            → consortium publish (four-eyes) + regulatory reporting path
  └─ track             → recovery_attempted / recovered_minor on transaction_outcomes
```

Put **value recovered** next to value prevented on the metrics strip. On a push rail recovery is
close to zero — which is §2.1's argument made visible in rupees, and the strongest possible case for
prevention.

---

**Next:** [05-GRAPH-CONSORTIUM.md](05-GRAPH-CONSORTIUM.md)
