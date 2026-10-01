# Nazar Console — Live Pages Guide

> **Purpose of this document:** A plain-English walkthrough of every page in the Nazar fraud-detection console. Written for presentations — no code jargon, just what each page shows, what the numbers mean, and what to point out when you're in front of a room.

---

## How the Console Is Organised

The sidebar groups all pages into four sections:

| Section | What it's for |
|---------|--------------|
| **Operate** | Day-to-day monitoring — watching payments flow, spotting problems, investigating individual transactions |
| **Verify** | Proving the system works — running test scenarios, exploring analytics, looking up past decisions |
| **Govern** | Controlling how the system behaves — tuning risk appetite, reviewing model performance, checking speed |
| **Customer** | What the actual bank customer sees on their phone |

---

## Operate Section

### 1. Command Centre (Home Page)

**What it is:** The main dashboard — the "big screen" you'd put up in a control room. Everything on this page is live and real.

**What you see:**
- **Live/Connecting indicator** — A glowing dot at the top telling you whether the system is actively streaming decisions right now.
- **Four headline numbers:**
  - **Value stopped or challenged** — Total money (in ₹) that the system stepped in on. This is not money "saved" — it's money that triggered some action (a warning, a hold, a block).
  - **Decisions per minute** — How many payments the system is judging right now. Shows the system is alive and processing.
  - **Decision time p99** — 99% of all payments are decided faster than this number (in milliseconds). Shows the system doesn't slow down banking.
  - **Intervention rate** — What percentage of payments the system actually interfered with. Lower is better — the system should leave most legitimate payments alone.

- **Live decisions feed** — A scrolling list of every payment being decided, showing:
  - Who sent it, who received it
  - How much money
  - What the system decided (Allowed, Warned, Held, Blocked, etc.)
  - How long the decision took

- **Attack launcher** — Buttons to fire simulated fraud attacks at the system. These send real payment requests through the real engine — they're not animations. If the system fails to catch an attack, it shows up honestly.

- **Phone connection tracker** — When someone scans the QR code from the Payer App page and uses the phone demo, this strip shows which step in the story the phone user is on, so the audience watching the big screen can follow along.

**How to read it:** This is your proof that the system runs live. Point at the feed — every row appeared because the engine just made a real decision. Click any row to see *why* the system decided that way.

---

### 2. Live Monitor

**What it is:** A more detailed version of the live feed, built for analysts rather than a projector screen.

**What you see:**
- **Same live stream** as Command Centre, but in a proper table with columns for Time, Payer, Beneficiary, Payment Channel, Amount, Decision, and Speed.
- **Metric cards** at the top for value prevented, total decisions, speed, and challenge rate.
- **Session breakdown** — A count of how many payments were Allowed, Warned, Held, Blocked, etc.
- **Dependencies panel** — Shows whether the databases the system relies on (Redis and Postgres) are up and how fast they respond.

**How to read it:** Click any row and it opens in the Investigation page with the full explanation. The dependencies panel is the first place to look if something seems wrong.

---

### 3. Anomaly Detection

**What it is:** Shows how the system spots payments that "don't look like anything we've seen before" — without needing to know what fraud looks like.

**What you see:**
- **Scatter chart** — Each dot is a payment. Dots near the bottom are unusual (the system thinks they don't match normal behaviour). Dots in the red band at the bottom are flagged as "novel" — meaning they look stranger than 95% of recent traffic.
- **p-value distribution** — A histogram showing how unusual payments were overall. In a healthy system, this should look roughly flat across the middle (most payments are normal) with a small spike at the left (the unusual ones).
- **"Most anomalous right now" table** — The strangest payments, ranked. Click any one to investigate.
- **Metric cards:** How many payments were evaluated, how many were flagged as novel, the novelty rate, and how many are still "warming up" (the system needs about 30 transactions before it can start judging what's normal).

**How to read it:** This detector doesn't need to be told what fraud looks like — it only learns what *normal* looks like and flags anything that doesn't fit. That's why it can catch brand-new types of fraud on the first day. It runs as a shadow (informational only) — it never blocks a payment by itself.

---

### 4. Alerts

**What it is:** A queue of payments that need human attention. Every time the system decides something other than "Allow," it creates an alert here.

**What you see:**
- **Filter tabs** — Switch between Open alerts, Resolved alerts, or All.
- **Alert table** showing when it was raised, how severe it is (low / medium / high / critical), what the decision was, who sent and received money, the amount, and a button to mark it resolved.

**How to read it:** This is the analyst's inbox. In a real bank, a fraud analyst would work through these one by one. Clicking the timestamp opens the full investigation for that payment. Resolving an alert is a real, permanent change — it's saved in the database, not just hidden on screen.

---

### 5. Investigation

**What it is:** The deep-dive page. Pick any single payment and see everything the system considered when it made its decision.

**What you see:**
- **Search bar** — Paste a payment reference number (end-to-end ID) or click a payment from any other page.
- **Full decision detail** including:
  - What was checked and what was found
  - The ranked evidence — which signals fired and how important each one was
  - The monetary arithmetic — how the system weighed the cost of being wrong against the cost of getting in the customer's way
  - The action ladder — showing where on the escalation scale this payment landed
  - A re-execution proof — the system re-runs the exact same decision to prove it produces an identical result, confirming nothing has been tampered with

**How to read it:** This is how you answer "why did the system do that?" for any specific payment. Every piece of evidence shown here was computed at the time of the original decision, not added afterwards.

---

### 6. Resilience

**What it is:** Shows what happens when parts of the system break — and proves the system keeps working anyway.

**What you see:**
- **Dependency cards** for Redis (fast in-memory store) and Postgres (permanent database), showing whether each is up and how fast it responds.
- **Chaos controls** — Real buttons that actually kill and restart the Redis database container. This is not a simulation — pressing "Kill Redis" really stops it.
- **Degradation banner** — When a dependency is down, this shows what the system does differently: it switches to "rails-only" mode (just the written rules, no machine learning), puts a cap on how much money it will let through, and never blocks a payment outright (because blocking someone's money when you can't check properly is worse than being cautious).

**How to read it:** This is your "what if something breaks?" slide. Kill Redis, show the system still answers, still makes decisions, just more cautiously. Then restore it and show everything recovers.

---

### 7. Audit Chain

**What it is:** Proves that no one has tampered with any past decision.

**What you see:**
- **Verify button** — Walks through every decision ever recorded, checking that each one is linked to the previous one in a sealed chain (like a chain of locks — break one link and everything after it looks wrong).
- **Metric cards:**
  - **Entries checked** — How many decisions were verified
  - **Last verified** — When you last ran the check
  - **Break point** — If any tampering is detected, this shows exactly where the chain broke. If it says "none," every decision is intact.
  - **Writers** — Always shows 1. Only one part of the system is allowed to write decisions, which prevents conflicting records.

**How to read it:** Press the button, wait a moment, and look at the result. "Chain intact" means every decision ever made by the system can be traced back and proven untouched. This is how a regulator or court would verify the bank's records.

---

## Verify Section

### 8. Fraud Analytics

**What it is:** A breakdown of *where* fraud is happening, *what kind* it is, and *how much money* is at risk — all computed from real decisions the system has made.

**What you see:**
- **Top-level numbers:** Total decisions analysed, how many were challenged, the total money flowing through, and the total value stopped or challenged.
- **Location heat map** — Which cities or areas have the most money at risk. Darker shading = more money exposed. The percentage shows how much of that money was caught.
- **By attack type** — Horizontal bars showing which types of fraud the system has seen, ranked by money (not by count, because a thousand tiny probes are less important than one big scam).
- **By amount band** — Which price ranges see the most fraud activity (small probes, everyday amounts, the scam sweet spot, large cash-outs).
- **Which detector caught it** — Shows which of the four detection layers was responsible for flagging each fraud.
- **By payment rail** — Which payment channels (UPI, NEFT, etc.) are seeing the most risk.
- **By hour of day** — When fraud activity peaks during the day.

**How to read it:** Everything is ranked by money, not by the number of payments. This is deliberate — a thousand ₹2 probe payments are noise, not the real problem. The real problem is the ₹50,000 scam hiding behind them.

---

### 9. Demo Runner

**What it is:** Pre-built test scenarios that fire real payments through the real engine and show you the result.

**The scenarios:**

| Scenario | What it does | Expected result |
|----------|-------------|-----------------|
| **A — Normal** | An ordinary payment between two known accounts | Allowed through without friction |
| **B — APP scam** | A payment to a new beneficiary with a scam pattern | The system warns the customer (step-up) |
| **C — Mule fan-out** | Multiple people paying into one suspicious collector account | Network/graph evidence fires |
| **D — Large new payee** | A big payment to someone you've never paid before | Payment gets capped (amount limited) |
| **E — Redis killed** | The database is deliberately destroyed mid-payment | System still answers, never blocks, degrades gracefully |
| **F — Legit merchant** | 30 different people paying one real shop | Ring score stays at zero (the system correctly recognises it's a real business, not a fraud ring) |
| **G — Round-trip** | A decision is saved and then re-read | Proves the saved record is byte-identical to the original |
| **H — Audit chain** | Walks the full hash chain from the very first decision | Proves nothing has been changed |

**How to read it:** Click "Run" on any scenario. Each one fires a real payment through the real decision engine. The pass/fail result tells you whether the system behaved as expected. Scenario E actually kills and restores the database — it's not pretending.

---

### 10. Time Machine

**What it is:** The same as the Investigation page, but framed for looking up old payments. Search for any past payment by its reference number and see exactly what the system recorded at the time — never re-computed, just the original record.

**How to read it:** This is how you prove what happened last week, last month, or last year. The data shown is what was stored at the time of the decision — the system never goes back and recalculates.

---

### 11. Graph / Ring

**What it is:** Shows the "network picture" around any account — who has been paying this account, and whether the pattern looks like a fraud ring or a legitimate business.

**What you see:**
- **Ring score** (0 to 1) — How much the payment pattern looks like a fraud collection ring. Zero means "looks normal," approaching 1 means "lots of unrelated people paying the same account, which is suspicious."
- **Ring size** — How many different people have paid this account. Above 25 distinct payers, the system assumes it's a real merchant, not a ring.
- **Component banks** — How many different banks the payers come from. Real fraud rings often recruit victims across many banks.
- **Hops to cash-out** — How many steps of forwarding it takes before the money reaches a final destination. More hops = more suspicious.
- **Device-shared degree** — How many accounts share the same phone or device. Fraud rings often operate multiple accounts from one device.

**How to read it:** Look up a suspicious account and check the ring score. If it's zero and the ring size is high, it's a real business. If the ring score is high with few payers, multiple banks, and shared devices — that's a collection ring. The graph layer never blocks a payment by itself; it provides evidence that other layers use.

---

## Govern Section

### 12. Policy Studio

**What it is:** Where you change *how aggressive* the system is — not by moving a "sensitivity slider," but by telling it what mistakes cost.

**What you see:**
- **Five cost controls:**
  - **Cost of holding a payment** — What it costs operationally when the system stops a payment for manual review. Raise this and the system holds fewer payments.
  - **Cost of warning the customer** — The cost of interrupting someone with a scam warning. Raise this and the system shows fewer warnings.
  - **Value of a customer we annoy** — The business we lose when a real customer gives up because we got in the way. Raise this and the system interferes less with legitimate users.
  - **Share of a UPI fraud we actually eat** — How much of a fraudulent UPI payment the bank never gets back. Raise this and fraud hurts more, so the system intervenes sooner.
  - **How often holding actually stops the fraud** — If holding a payment rarely prevents the loss, the system stops choosing to hold.

- **Preview panel** — After moving any slider, press "Preview against real traffic" and the system re-runs recent real decisions under the new settings. It shows you exactly which payments would have been treated differently:
  - How many would now be challenged that weren't before
  - How many would now go through that were previously stopped
  - The money value of each change
  - A line-by-line list of every payment that would flip

- **Make Live button** — Applies the new policy to all future decisions.

**How to read it:** This page shows that risk appetite is not a magic number — it's a set of business costs. Changing one cost ripples through every future decision. The preview lets you see the impact before committing.

---

### 13. Model Evidence

**What it is:** All the numbers about how well the system's detection methods perform — with honesty about where each number comes from.

**What you see:**
- **The four detection layers explained:**
  1. **Written rules & regulatory rails** — Hard-coded legal requirements. No machine learning. If the law says cool-off periods apply, they apply.
  2. **Supervised model** — A machine-learning model trained on past fraud. Excellent at recognising patterns it has seen before. Blind to anything genuinely new.
  3. **Behavioural anomaly detector** — Learns what "normal" looks like, flags anything that doesn't fit. Never needs to see fraud examples. Catches new attacks from day one.
  4. **Beneficiary network analysis** — Looks at the shape of payment networks. A collector account fed by many unrelated payers sharing devices looks like a ring, regardless of the amounts.

- **Performance numbers with provenance tags:**
  - **[MEASURED]** — Numbers observed on this live, running system or on independently labelled test data.
  - **[RECOVERED]** — Numbers measured on the system's own test data. Real evaluation, but the system generated the fraud labels itself.

- **Live speed measurements** — How fast decisions are made on this running system (not a benchmark, the actual running times).
- **Ablation study** — What happens when you remove each detection layer one at a time. Shows that each layer earns its place.
- **Defence-in-depth coverage** — After you run attack campaigns, this shows which detector caught what. Three of the four don't need fraud labels, which is the core argument for why the system can catch new types of fraud.

**How to read it:** The key message is "four detectors, not one model." The ML model is just one of four, and it's the only one that needs past fraud examples. The other three work structurally, so they catch things the model has never seen.

---

### 14. Calibration

**What it is:** Shows how the system's probability scores are calibrated — meaning that when the system says "there's a 30% chance this is fraud," it's actually fraud about 30% of the time.

**What you see:**
- **Calibrator method and version** — Which calibration approach is in use.
- **Train prevalence vs. Natural prevalence** — The training data was deliberately overloaded with fraud examples to help the model learn. The "natural prevalence" is the actual real-world fraud rate, which is much lower. The system adjusts for this difference so that its scores match reality.
- **Score distribution** — A histogram showing how the system is scoring current traffic. In a well-calibrated system, most scores should be very low (most payments are legitimate) with a small tail of higher scores (the suspicious ones).

**How to read it:** Calibration matters because the system uses these probabilities to do cost arithmetic. If the probabilities are wrong, the arithmetic produces the wrong decisions. A well-calibrated system means the cost-based decision-making actually works as intended.

---

### 15. Latency

**What it is:** A dedicated page showing exactly how fast the system makes decisions.

**What you see:**
- **Four speed metrics:**
  - **p50 (median)** — Half of all decisions are faster than this
  - **p99** — 99% of decisions are faster than this
  - **p99.9** — 99.9% of decisions are faster than this
  - **Max** — The single slowest decision in the measurement window
- **Full percentile table** — All five percentile values in a table.

**How to read it:** These are real measurements from the actual running system, not benchmarks. They update every 5 seconds. A decision that takes more than about 50 milliseconds would start being noticeable to customers — so these numbers should all be well below that. The system is designed to make every decision in single-digit milliseconds.

---

## Customer Section

### 16. Payer App (QR Launcher + Phone Experience)

This is actually two pages working together:

#### The QR Launcher (admin side)

**What it is:** Shows a QR code on the big screen. When someone in the audience scans it with their phone, they get the Payer App — a simulated banking app experience.

**What you see:**
- A large QR code
- The URL the QR encodes
- Explanation of what's real about the demo

#### The Phone Experience (what the audience member sees)

**What it is:** A five-act interactive story where the person holding the phone plays a bank customer.

**The five acts:**

1. **Everyday** — The person is introduced as a character (e.g., "You are Priya Sharma"). They make a normal small payment — coffee, groceries. The system allows it silently in milliseconds. Point: legitimate customers should never notice the fraud system.

2. **The Approach** — The person receives a convincing scam message (one of five real Indian fraud types, rotated so each demo is different). They choose: ignore it, or follow the scammer's instructions.

3. **The Catch** — If they follow the scammer and try to pay, the system intervenes with a clear, plain-language warning. The warning lists the specific reasons the system flagged the payment ("You've never paid this account before," "Many unrelated people are paying this same account"). The person can also tap "Why do you think that?" to see the full evidence.

4. **The Choice** — The person can cancel the payment (money stays safe) or override the warning and send anyway (the payment gets flagged to an analyst queue). Point: the system warns rather than blocking — because silently blocking a customer's money is a different kind of harm.

5. **The Proof** — The person looks up at the big screen (Command Centre) and sees their payments already there — both the legitimate one and the scam one, with full evidence, the cost arithmetic, and a tamper-proof audit record.

**How to read it:** This is the centrepiece demo. Everything the phone shows is a real response from the real engine. If the engine made a mistake (allowed a scam payment through), the phone says so honestly rather than faking a warning. The warning reasons come directly from the engine's findings, translated into plain language. Each new scan generates a different scam story, so a second judge in the room doesn't watch a rerun.

---

## Quick Reference: Page URLs

| Page | URL Path |
|------|----------|
| Command Centre | `/#/` (home) |
| Live Monitor | `/#/feed` |
| Anomaly Detection | `/#/anomaly` |
| Alerts | `/#/alerts` |
| Investigation | `/#/investigate` |
| Resilience | `/#/resilience` |
| Audit Chain | `/#/audit` |
| Fraud Analytics | `/#/analytics` |
| Demo Runner | `/#/demo` |
| Time Machine | `/#/time-machine` |
| Graph / Ring | `/#/graph` |
| Policy Studio | `/#/policy-studio` |
| Model Evidence | `/#/model` |
| Calibration | `/#/calibration` |
| Latency | `/#/latency` |
| Payer App (QR) | `/#/payer-app` |
| Payer App (Phone) | `/#/pay` |

---

## Key Talking Points for Your Presentation

1. **Nothing is faked.** Every number, every decision, every row in the feed comes from the real running engine. If you launch an attack and the system fails to catch it, that failure shows up on screen — it doesn't pretend to succeed.

2. **Four detectors, not one model.** Only one of the four detection layers needs past fraud data. The other three work from the structure of transactions, which is why the system can catch types of fraud it has never seen before.

3. **Speed matters.** The entire decision — across all four detectors, the cost arithmetic, the audit record — typically completes in under 10 milliseconds. Customers never notice.

4. **Risk appetite is expressed in money, not thresholds.** The Policy Studio doesn't have a "sensitivity dial." Instead, it asks: "What does it cost when we're wrong?" Change those costs and every decision re-prices accordingly.

5. **Transparency is built in.** Every decision can be explained (Investigation), re-run to prove it reproduces identically (Audit Chain), and traced back through a tamper-proof record. The customer can even see the reasons on their phone, in plain language.

6. **Graceful degradation.** Kill the database — the system keeps answering. It falls back to rules-only mode, caps how much money it lets through, and never blocks outright. The moment the database returns, everything resumes.
