# Nazar

Real-time payments fraud detection prototype. Production-shaped architecture, honestly
tiered claims (`[MEASURED]` / `[RECOVERED]` / `[MODELLED]`), built to the spec in
[`docs/00`–`docs/08`](docs/00-ARCHITECTURE.md). See [`CLAUDE.md`](CLAUDE.md) for the
operating contract and non-negotiables this build is held to.

## What's real here

Every core claim in the architecture has a working, exercised implementation — not a stub
behind a demo button:

- **Decision engine**: local filters → regulatory rails → trusted-pair fast path →
  calibrated model score → expected-cost minimisation → policy rails → capped advisory
  attachment (docs/04). CEL rules (`rules/`), never `eval`.
- **Redis profile store**: hash-tagged keys, `ZCOUNT`-based windows (never `ZCARD`),
  read-strictly-before-write, named-not-positional pipeline binding (docs/02).
- **Trained model**: LightGBM (`py/training/`), monotone constraints from the feature
  registry, beta calibration, prevalence correction — loaded live via the pure-Go `leaves`
  library, no Python in the request path.
- **Graph/ring signal**: in-process Go adjacency with decay and a component-size cap —
  `test_merchant_is_not_a_ring` passes (500 payers → one merchant → zero ring score) and a
  small device-linked forwarding cluster scores nonzero.
- **Novelty**: feature-space k-NN + conformal p-value, ships in `shadow` — computed and
  recorded, never influences the decision.
- **Consortium**: HMAC+epoch pseudonymous tokens, report/retract/dispute, per-reporter hash
  chains, legal-entity collapse for the ≥2-independent-reporters rail, a real
  `GET /v1/federation/wire/{id}` endpoint returning literal signed bytes.
- **Audit chain**: SHA-256 hash chain over every decision, survives process restart (chain
  state resumes from Postgres), a real `Verify Chain` that recomputes and compares.
- **Resilience**: a self-enforced decision deadline, and a "Kill Redis" control that
  actually stops the container (`podman stop`) — the degraded path is the real code path, not
  a simulated flag.
- **Demo runner**: 8 scripted scenarios (A–H), each calling the exact same decision function
  the live API uses.

## P0 simplifications (intentional, labelled — see `CLAUDE.md`'s table)

One audit chain / one writer · plain Redis pipelines (no Redis Functions) · fixed
concurrency, no adaptive limiter · in-process graph (no sharding) · HMAC+epoch consortium
tokens (not OPRF — see `docs/05 §4.1` for the honest framing) · `leaves` ablation-based
feature attribution (not exact TreeSHAP — `leaves` doesn't expose per-tree leaf indices) ·
no LLM narrator lane yet (Milestone 8, out of scope for this pass).

## Quickstart

Requires: Go 1.22+, Python 3.11+, Node 20+, and either `podman` (rootless, no daemon needed —
what this was built and tested against) or a running `docker` daemon.

```bash
make setup              # starts Redis + Postgres, applies migrations
make generate           # synthetic data: 2k accounts, 90d warmup, 5 typologies (~15s)
make train              # trains the P0 LightGBM model on generated data (~20s)
make dev                # starts the backend on :8080
```

In another terminal:

```bash
make console-dev        # starts the React console on :5173 (or next free port)
```

Then either click through the console's **Demo Runner** screen, or run the scripted suite
directly:

```bash
make demo               # fires scenarios A-H against the real /v1/decide path
```

### Optional: real-dataset GPU validation

```bash
make validate-ulb       # downloads the ULB credit-card-fraud dataset, trains XGBoost on
                         # GPU (device=cuda), reports PR-AUC — a [MEASURED] sanity check on
                         # the training methodology, not a claim about payment fraud
```

### Tests

```bash
make test               # go build, go vet, and the full invariant suite (go/test/invariants)
```

The invariant suite is the architecture in executable form (docs/06 §4): window arithmetic
matches a brute-force reference, no BLOCK survives under any degradation, advisories never
escalate past the policy cap, a decision is always answered within the self-enforced
deadline, every feature has a backing key, `internal/features` has zero I/O, replay is
byte-exact, and more.

## Repository layout

See [`docs/00-ARCHITECTURE.md §9`](docs/00-ARCHITECTURE.md#9--repository-layout) for the
canonical layout this follows. Top-level:

```
go/            decision service (one P0 binary, four logical goroutine pools)
py/generator/  synthetic transaction generator (population, warmup, typologies)
py/training/   feature replay + LightGBM training + beta calibration
py/eval/       ULB real-dataset validation (GPU)
console/       React/TS/Tailwind operator console (brand kit: docs/08)
features/      registry.yaml — the feature catalogue, shared by Go and Python
policy/        versioned decision policy bundles
rules/         versioned CEL rule bundles
sql/migrations/
docs/          the build spec
```

## Honest claims

Every number this build produces is tagged `[MEASURED]`, `[RECOVERED]`, or `[MODELLED]`
per `docs/06 §5`'s claims register. In short: latency and calibration numbers measured on
this machine are real measurements of this prototype; anything about "detection rate" is
recovery of this repo's own synthetic generator, not a claim about real-world fraud.
