# 05 — Graph, Rings, and the Consortium

**Fixes:** F-26 F-27 F-28 F-29 F-30 F-31 F-51 F-52 F-53 F-54 F-55

Two layers with the most severe findings in the review. Both are kept — the ideas are right — with
the parts that were ported from a different domain re-derived for payments.

---

## 1 — Why the graph exists

`PLAYBOOK §06`'s typology matrix is the correct argument and is kept: APP scam has exactly two
strong columns, and both are graph. Nothing about the payer is suspicious; everything about the
payee is. That is why this layer is load-bearing rather than impressive.

What changed is **how a graph signal is derived**.

---

## 2 — The ring weight table, re-derived

### 2.1 — The flaw

The previous design ported Satyum's identifier weights verbatim and called it a strength
(*"maps almost one-to-one onto payments"*, *"Port it as-is"*):

| Identifier | Weight |
|---|---|
| `creditor_account` (shared beneficiary) | **1.0** |
| `device_id` | 0.9 |

with `ring_weight_threshold = 1.0` and `min_ring_size = 3`, wired directly to a hard `BLOCK`.

**So: any three unrelated payers sharing one beneficiary form a confirmed ring and get blocked.**

In Satyum's domain (loan applications sharing a payout account) shared-payout is near-dispositive.
In payments, a shared `creditor_account` across unrelated payers is the definition of a merchant, a
utility, a school, a landlord, a mutual fund — **every legitimate collection account in the
country**. Three people paying the same electricity board are blocked.

The document's own defence — *"won't this flag everyone who banks with the same PSP?"* — answers the
wrong objection with confidence. The fatal question in payments is *"won't this flag everyone who
pays the same merchant?"*, and the answer as designed is **yes, by design.**

### 2.2 — The fix: weight is a function of the identifier's frequency and age

An identifier's evidential value is inversely related to how many people legitimately share it. That
is the log-likelihood-ratio intuition, and it makes the weight a *function*, not a constant.

```go
// Shared-beneficiary edge weight. High only when the beneficiary is BOTH rare AND new.
func weightSharedCreditor(payee PayeeStats) float64 {
    if payee.DistinctPayers30d > 200 { return 0.0 }        // a merchant. Not evidence.
    rarity := 1.0 / math.Log2(2+float64(payee.DistinctPayers30d))
    recency := clamp01(1.0 - float64(payee.FirstSeenByUsDays)/90.0)
    fwd := clamp01(payee.FwdRatio1h)                        // pass-through behaviour
    return clamp(rarity*(0.5+0.5*recency)*(0.4+0.6*fwd), 0, 1.0)
}
```

| Case | `DistinctPayers30d` | `FirstSeenByUsDays` | `FwdRatio` | Weight | Right? |
|---|---|---|---|---|---|
| Electricity board | 50,000 | 900 | 0.0 | **0.00** | ✅ hard zero |
| Popular local merchant | 400 | 200 | 0.1 | **0.00** | ✅ |
| Small merchant | 60 | 400 | 0.2 | 0.10 | ✅ low |
| **Mule collecting** | **11** | **3** | **0.94** | **0.27** | ✅ signal |
| Mule + shared device (0.9) | | | | **1.17 → ring** | ✅ |

The mule case alone (0.27) does **not** form a ring; combined with a shared device it does. That is
the discrimination principle the previous design wanted, now applied to the identifiers that
actually discriminate in this domain.

### 2.3 — Full weight table

| Identifier | Weight | Note |
|---|---|---|
| `pan` | 1.0 | Same individual. Genuinely dispositive |
| `device_id` | 0.9 | Same physical device across accounts |
| `creditor_account` | **`f(degree, age, fwd)` — 0.0 to 1.0** | ← re-derived (§2.2) |
| `debtor_account` | 0.9 | Same source |
| `phone` | 0.7 | Reassignable, but strong |
| `asn` | 0.05 | ← lowered. Carrier-grade NAT means millions share an ASN |
| `psp_handle` | 0.05 | ← lowered. Millions share a PSP |
| `geo_cell` | 0.0 | ← **dropped.** At any usable resolution this is population density |

**The defensible version of the discrimination sentence:**

> "A shared PSP handle is worth 0.05 against a threshold of 1.0 — it can never form a ring. A shared
> beneficiary is worth between zero and one *depending on how many people pay that beneficiary*: the
> electricity board is a hard zero, a three-day-old account collecting from eleven unrelated payers
> and forwarding 94% within a minute is a real signal. Weight is a function of how surprising the
> sharing is, not a constant — otherwise you flag everyone who pays the same merchant."

---

## 3 — Rings: decay, caps, and never a block

### 3.1 — The three structural fixes

**(a) Decay and re-partition (fixes F-28).** Union-Find is monotone — it never un-merges — so over a
30-day window with any weight-1.0 identifier the payment graph collapses into one giant component
containing every active account. Replace it with an incrementally maintained weighted graph:

```
  edge weight decays exponentially, half-life 7 days
  edges below 0.05 are dropped
  components recomputed over edges above threshold, incrementally, every 60 s
```

**(b) Hub detection (fixes F-28).** A component above `max_component_size` (default 500) is a **hub,
not a ring**. It is labelled as such, excluded from ring signals, and surfaced for review — because
a genuine 500-account ring and a mis-weighted merchant look identical to a component-size check, and
the operator should see which one it is.

**(c) Rings never hard-block (fixes F-27).** `ring_flagged` is:

- a **continuous feature** inside the model (`ring_score`, `ring_size`, `component_bank_count`)
- a **case-opening signal** at high priority
- a **friction escalator**, capped like any advisory

`ring_confirmed`, the only thing that touches a blocklist, means **analyst-dispositioned with
four-eyes approval**. It comes from Postgres, not from `graph/metrics`. The previous design read it
straight off an unsupervised clustering result and hard-blocked on it, with "confirmed" never defined
anywhere.

### 3.2 — Bounded traversal, off the hot path

`hops_to_cashout` was unbounded BFS over a giant component, scheduled onto the scoring event loop
(F-29). Now:

- Runs in the **graph service** — its own process, its own scheduler
  ([00-ARCH §5](00-ARCHITECTURE.md#5--service-decomposition))
- Depth ≤ 3, node-visit budget ≤ 5,000, **hard 50 ms deadline**; exceeded → `NOT_EVALUATED`
- Written to the profile store; read as an O(1) lookup on the hot path
- **`graph_staleness_s` ships as a companion feature** on every graph value, so the model can learn
  to discount stale signal, and the decision record shows exactly how fresh each part of the vector
  was (D2)

### 3.3 — The staleness attack, named

Graph staleness grows with load. Attacks arrive in bursts. So graph features degrade *precisely
during an attack*, and an attacker who can generate load — trivially, via card testing, which the
system is supposed to detect — can degrade the graph lane deliberately and push the real fraud
through (F-30).

Controls: staleness is a **feature** (so the model discounts it), a **metric** (so it alarms), and
above a policy threshold graph features go `NOT_EVALUATED` rather than being silently trusted. The
previous design claimed the staleness was "bounded and recorded"; it was neither.

### 3.4 — Sizing, stated

| | `[P0]` | `[P1]` (modelled) |
|---|---|---|
| Nodes | ~5k | ~25M |
| Edges (30d, post-decay) | ~40k | ~600M |
| Store | in-process Go adjacency | sharded, RocksDB-backed, incremental components |
| Component recompute | < 5 ms | seconds, incremental |

At `[P2]` this needs a real graph engine with incremental community detection. That was the previous
design's honest answer and it is kept — but now it sits under an actual node/edge count instead of
"a few thousand" versus "national scale" (F-31).

---

## 4 — Consortium

The layer with the most severe security finding. The design is kept; three claims are corrected.

### 4.1 — The token claim, corrected

The previous wire protocol:

```json
"token": "HMAC-SHA256(pepper, 'creditor_account:501001234')"
```

described as *"non-invertible, enumeration-resistant."*

**True for the registry operator. False for every member** — and the document does not distinguish
them (F-51). Indian account numbers are structured and low-entropy (9–16 digits, IFSC-scoped,
near-sequential within a branch). Any member holding the shared pepper can enumerate a target bank's
range in minutes and invert the entire registry.

Consequence: **Bank A recovers Bank B's complete confirmed-fraud customer list** — the exact outcome
the DPDP argument exists to prevent. Aggravated by a single shared pepper (one compromise
deanonymises everything, retroactively) with no rotation path (no key epoch in the wire format).

**Two honest options. Pick one and say which.**

| | Mechanism | Property | Cost |
|---|---|---|---|
| **A — pseudonymisation, stated honestly** | Shared pepper, **rotated by epoch** (`ep` field in the wire format), members legally bound by the consortium agreement | Operator cannot invert. **Members can.** This is what credit bureaus actually do, and it is a real control | Free |
| **B — 2HashDH OPRF** ⟵ recommended if you claim privacy | Querier blinds the identifier; registry applies its key; querier unblinds. No member can compute tokens offline | Members cannot enumerate. Operator does not learn the identifier | ~150 LOC, one EC op (~50 µs), one extra RTT — off the hot path, so it costs nothing that matters |

Option B is roughly a day of work with a curve25519 library and it converts a claim you cannot
defend into one you can. **If you are not doing B, do not say "non-invertible."** Say:

> "It's a pseudonym, not a confidentiality control. The registry operator can't invert it. Members
> can, because they hold the pepper and account numbers are low-entropy — that's what the consortium
> agreement is for, and it's what credit bureaus do. If you want members unable to enumerate, that's
> an OPRF, it's about a day of work, and here's the interface it sits behind."

**Naming discipline, kept verbatim from the previous design because it was right:** call this
**tokenised set membership**, not PSI. It is not DH/OPRF Private Set Intersection and it does not
hide the querier's lookup tokens from the operator. A crypto-literate judge who hears "PSI" and then
sees an HMAC lookup will take more from you than the word gained.

### 4.2 — Revocation, expiry, dispute

The previous protocol had exactly one operation: `report`. No retract, no expiry, no dispute — so a
false report blocks a payee at every participating institution **permanently, with no mechanism to
undo it** (F-52). A national blocklist with no off-switch is a customer-harm machine, and grievance
redressal is precisely what makes a shared blocklist legally operable.

```
  report   → publish a token with a threat class and a mandatory TTL (default 180d)
  retract  → reporter withdraws; effective immediately at all members
  dispute  → any member or the operator flags an entry; it drops to advisory-only pending resolution
  expire   → automatic at TTL; renewal requires an affirmative re-report
  decay    → an entry's weight decays with age even before expiry
```

Every entry carries `expires_at`. There is **no permanent entry.**

### 4.3 — The `>= 2 reporters` rail, made to mean something

`ARCHITECTURE §16` says reputation gates the rail; `§09` reads a raw integer. Nothing enforced
independence (F-53). Enforced now:

```go
func consortiumRailFires(entries []Entry, pol Policy) bool {
    byEntity := map[LegalEntityID]bool{}
    for _, e := range entries {
        if !e.SignatureValid() || e.Expired() || e.Disputed() { continue }
        if reputation(e.Reporter) < pol.MinReporterReputation { continue }
        byEntity[registry.LegalEntity(e.Reporter)] = true   // ← collapses BINs/subsidiaries
    }
    return len(byEntity) >= 2
}
```

`registry.LegalEntity` maps participant codes to **legal entities**, so two BINs of the same bank, or
a bank and its subsidiary, count once. Without that mapping "two independent institutions" is a
phrase, not a control.

Reporter reputation: `confirmed / (confirmed + dismissed)`, time-decayed, with a floor and a
cold-start prior. Every entry is signed, so a reporter whose flags are later dismissed has a
**provable** track record — which is why entries are signed rather than merely hashed. That reasoning
was correct in the previous design and is kept.

### 4.4 — Chains without consensus

The previous documents described three incompatible mechanisms — a Bloom filter, a signed
append-only log, and a `prev_hash` chain — and never reconciled them (F-55). A hash chain needs a
total order; across mutually distrusting institutions that needs consensus, which nobody specified,
and if the operator assigns order then there *is* a trusted operator and the pitch is false.

**Per-reporter chains + a published Merkle root.** No global order, no consensus, no trusted operator:

```
  Each reporter chains only its OWN entries:   h_i = SHA256(h_{i-1} ‖ canonical_cbor(entry))
  Each reporter signs its head periodically.
  The registry publishes a Merkle root over all reporter heads every interval, signed and chained.

  ⇒ A reporter cannot rewrite its own history       (its chain breaks)
  ⇒ The operator cannot forge or drop an entry      (the reporter's signature and head disagree)
  ⇒ Ordering across reporters is partial, by interval — and that is sufficient for tamper-evidence
```

**Say the ordering property precisely.** You get tamper-evidence, not a total order. You do not need
a total order; claiming one requires consensus you have not built.

### 4.5 — Bloom filters are a negative cache only

A Bloom filter has false positives by construction. On a blocking path, that blocks a randomly chosen
innocent payee with a confident cross-institutional reason attached (F-54).

```
  filter miss → definitively not listed → skip the round trip
  filter hit  → MAYBE listed → confirm exactly against blk:<token> before any rail fires
```

At `[P1]`, a 0.1% FPR means ~430k false hits/day, each costing one extra Redis GET and **zero
customer impact**. The previous plan's *"build the Bloom filter first — it works standalone and
demos in thirty seconds"* is fine as a build order; it is not fine as an authority.

### 4.6 — The wire format

```json
{
  "v": 2,
  "op": "report",
  "ep": 3,
  "token": "9f3a…c2e1",
  "kind": "creditor_account",
  "threat_class": "mule_beneficiary",
  "reporter": "BANK_A",
  "legal_entity": "LEI-549300ABCDEF",
  "reported_at": "2026-08-14T10:31:02Z",
  "expires_at":  "2027-02-10T10:31:02Z",
  "chain_seq": 40219,
  "prev_hash": "7b21…",
  "hash": "e40c…",
  "sig": "MEUCIQ…"
}
```

New vs. the previous version: `ep` (pepper epoch — makes rotation possible), `legal_entity`
(independence), `expires_at` (no permanent entries), `chain_seq` (per-reporter chain), and `op`
extended to `report | retract | dispute`.

**What does not cross:** account numbers, VPAs, names, phone numbers, amounts, device IDs, customer
activity timestamps. That part of the previous design was right and is unchanged.

`GET /v1/federation/wire/{id}` returns the literal bytes. Build it — it costs ten lines and converts
a claim into an artifact. Best small idea in the original document.

### 4.7 — DPIP positioning: verify before you say it

`ARCHITECTURE §00` builds the pitch on DPIP being announced-but-unlaunched *"as of early 2026."*
**It is August 2026** (F-76). If DPIP has since launched, the positioning inverts from opportunity to
liability, live on stage.

Re-verify against primary sources the week of the pitch:

| Claim | Verify against |
|---|---|
| DPIP status | rbihub.in / RBI press releases — **not** news aggregators |
| Authentication Directions in force, and that they sanction risk-based auth | **The RBI circular itself.** Currently cited to two law-firm notes and a vendor blog — for the single most load-bearing claim in the deck |
| UPI volumes | NPCI statistics page. And fix the arithmetic: 23.2B/31 = **748M/day = 8,660 TPS average**, not "above 5,000" (F-75) |
| MuleHunter adoption | Verify currency. And **stop quoting "~95% accuracy"** — it is meaningless at a sub-1% base rate, and the same deck spends a page explaining why (F-77) |

**And if DPIP has launched, the pitch gets better, not worse:** you built a member implementation of
the thing the regulator just shipped, with a wire format and a working two-instance demo. Prepare
both framings.

---

**Next:** [06-BUILD-PLAN.md](06-BUILD-PLAN.md)
