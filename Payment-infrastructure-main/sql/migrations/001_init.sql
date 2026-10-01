-- Nazar P0 schema. See docs/02-DATA-AND-FEATURES.md §7.
-- Money is always int64 minor units (paise). Never a float.

CREATE TABLE IF NOT EXISTS participants (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO participants (id, name) VALUES
  ('BANK_A', 'Bank A (Nazar host)'),
  ('BANK_B', 'Bank B (consortium peer)')
ON CONFLICT DO NOTHING;

-- ══════════════════════════════════════════════════════════════════════
--  TRANSACTIONS — daily partitions
-- ══════════════════════════════════════════════════════════════════════
CREATE TABLE IF NOT EXISTS transactions (
  end_to_end_id     TEXT        NOT NULL,
  accepted_at       TIMESTAMPTZ NOT NULL,
  event_ts          TIMESTAMPTZ,                    -- producer-claimed, not authoritative
  rail              TEXT        NOT NULL,
  channel           TEXT        NOT NULL,
  bank_instance     TEXT        NOT NULL REFERENCES participants(id),
  debtor_account    TEXT        NOT NULL,
  creditor_account  TEXT        NOT NULL,
  creditor_vpa      TEXT,
  creditor_ifsc     TEXT,
  amount_minor      BIGINT      NOT NULL CHECK (amount_minor > 0),
  currency          CHAR(3)     NOT NULL DEFAULT 'INR',
  device_id         TEXT,
  ip                INET,
  asn               INTEGER,
  geo_cell          TEXT,
  session_id        TEXT,
  initiation        TEXT,
  remittance_hash   BYTEA,        -- hash only. Raw attacker text is NOT stored here (B5)
  schema_version    INTEGER     NOT NULL,
  PRIMARY KEY (end_to_end_id, accepted_at)
) PARTITION BY RANGE (accepted_at);

CREATE TABLE IF NOT EXISTS transactions_default PARTITION OF transactions DEFAULT;

CREATE INDEX IF NOT EXISTS transactions_debtor_idx ON transactions (debtor_account, accepted_at DESC);
CREATE INDEX IF NOT EXISTS transactions_creditor_idx ON transactions (creditor_account, accepted_at DESC);

-- ══════════════════════════════════════════════════════════════════════
--  DECISIONS — append-only, MULTIPLE ROWS PER TRANSACTION
-- ══════════════════════════════════════════════════════════════════════
DO $$ BEGIN
  CREATE TYPE decision_kind AS ENUM ('LIVE','SHADOW','REPLAY','RESOLUTION','CONTROL');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS decisions (
  end_to_end_id        TEXT          NOT NULL,
  decision_seq         INTEGER       NOT NULL,       -- 0 = first live decision
  kind                 decision_kind NOT NULL,
  decided_at           TIMESTAMPTZ   NOT NULL,
  accepted_at          TIMESTAMPTZ   NOT NULL,

  action               TEXT NOT NULL,        -- ALLOW|ALLOW_MONITOR|STEP_UP|
                                             -- STEP_UP_INTERSTITIAL|HOLD|BLOCK|CAP
  pre_advisory_action  TEXT NOT NULL,        -- what OUR data alone concluded
  rail_fired           TEXT,
  reason_codes         TEXT[] NOT NULL,

  p_model              DOUBLE PRECISION,     -- calibrated. p_final ≡ p_model
  p_prevalence_adj     DOUBLE PRECISION,     -- after prior correction
  expected_loss_minor  BIGINT,
  expected_cost_minor  BIGINT,               -- incl. friction — the real objective

  features             JSONB NOT NULL,       -- hot partitions only; archived to Parquet at P1
  feature_status        JSONB NOT NULL,       -- per feature: OK|NA|NOT_EVALUATED + reason (D5)
  feature_staleness     JSONB NOT NULL,       -- per source: seconds stale at decision time (D2)
  contributions         JSONB,                -- exact TreeSHAP / linear contributions
  findings              JSONB NOT NULL,

  -- reproducibility: every dial that produced this row
  model_bundle_version    TEXT NOT NULL,
  policy_version           TEXT NOT NULL,
  rules_version            TEXT NOT NULL,
  signal_registry_version  TEXT NOT NULL,

  -- feedback-loop controls
  is_control           BOOLEAN NOT NULL DEFAULT false,
  action_propensity    DOUBLE PRECISION,     -- P(this action | policy) for off-policy eval

  degraded             TEXT[] NOT NULL DEFAULT '{}',
  total_ms             DOUBLE PRECISION NOT NULL,
  queue_delay_ms       DOUBLE PRECISION NOT NULL,
  service_ms           DOUBLE PRECISION NOT NULL,

  -- audit chain, per shard
  decision_shard       SMALLINT NOT NULL,
  chain_seq            BIGINT   NOT NULL,
  prev_hash            BYTEA,
  hash                 BYTEA    NOT NULL,
  checkpoint_id        BIGINT,

  -- decided_at must be in the PK: Postgres requires every unique constraint on a
  -- partitioned table to include the partition key column.
  PRIMARY KEY (end_to_end_id, decision_seq, decided_at)
) PARTITION BY RANGE (decided_at);

CREATE TABLE IF NOT EXISTS decisions_default PARTITION OF decisions DEFAULT;

CREATE UNIQUE INDEX IF NOT EXISTS decisions_chain_idx ON decisions (decision_shard, chain_seq, decided_at);
CREATE INDEX IF NOT EXISTS decisions_live_idx ON decisions (decided_at DESC) WHERE kind = 'LIVE';
CREATE INDEX IF NOT EXISTS decisions_action_idx ON decisions (action, decided_at DESC) WHERE kind = 'LIVE';

-- ══════════════════════════════════════════════════════════════════════
--  OUTCOMES — what actually happened
-- ══════════════════════════════════════════════════════════════════════
CREATE TABLE IF NOT EXISTS transaction_outcomes (
  end_to_end_id     TEXT PRIMARY KEY,
  settled           BOOLEAN,
  settled_at        TIMESTAMPTZ,
  settled_amount_minor BIGINT,               -- differs from instructed when CAP applied
  step_up_issued    BOOLEAN NOT NULL DEFAULT false,
  step_up_result    TEXT,                    -- PASSED|ABANDONED|FAILED|EXPIRED
  step_up_latency_ms INTEGER,
  step_up_attempts  SMALLINT,
  interstitial_shown BOOLEAN NOT NULL DEFAULT false,
  interstitial_result TEXT,                  -- PROCEEDED|CANCELLED
  recall_attempted  BOOLEAN NOT NULL DEFAULT false,
  recall_result     TEXT,
  recovered_minor   BIGINT NOT NULL DEFAULT 0,
  updated_at        TIMESTAMPTZ NOT NULL
);

-- ══════════════════════════════════════════════════════════════════════
--  LABELS
-- ══════════════════════════════════════════════════════════════════════
CREATE TABLE IF NOT EXISTS labels (
  end_to_end_id TEXT PRIMARY KEY,
  label         BOOLEAN     NOT NULL,
  source        TEXT        NOT NULL,   -- ANALYST|CHARGEBACK|CONFIRMED_MULE|VICTIM_REPORT|LEA
  confidence    REAL        NOT NULL DEFAULT 1.0,
  labelled_at   TIMESTAMPTZ NOT NULL,
  available_at  TIMESTAMPTZ NOT NULL,   -- when this label would REALISTICALLY be known
  labelled_by   TEXT,
  superseded_by BIGINT                  -- labels get revised; keep the history
);
CREATE INDEX IF NOT EXISTS labels_available_idx ON labels (available_at);

CREATE OR REPLACE VIEW labels_matured AS SELECT * FROM labels WHERE available_at <= now();

-- ══════════════════════════════════════════════════════════════════════
--  CASES
-- ══════════════════════════════════════════════════════════════════════
CREATE TABLE IF NOT EXISTS cases (
  id                  BIGSERIAL PRIMARY KEY,
  opened_at           TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL,
  status              TEXT NOT NULL,
  typology            TEXT NOT NULL,
  anchor_kind         TEXT NOT NULL,
  anchor_id           TEXT NOT NULL,
  exposure_minor      BIGINT NOT NULL,      -- rolling, recomputed as alerts join
  sla_due_at          TIMESTAMPTZ,
  assigned_to         TEXT,
  ring_id             BIGINT,
  narrative           TEXT,
  narrative_version   INTEGER NOT NULL DEFAULT 0,
  narrative_source    TEXT NOT NULL DEFAULT 'TEMPLATE'
);
CREATE INDEX IF NOT EXISTS cases_queue_idx ON cases (status, exposure_minor DESC);

CREATE TABLE IF NOT EXISTS alerts (
  id            BIGSERIAL PRIMARY KEY,
  case_id       BIGINT REFERENCES cases,
  end_to_end_id TEXT NOT NULL,
  decision_seq  INTEGER NOT NULL,
  decided_at    TIMESTAMPTZ NOT NULL,
  raised_at     TIMESTAMPTZ NOT NULL,
  severity      TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'open',   -- open | resolved
  resolved_at   TIMESTAMPTZ,
  resolved_by   TEXT,
  FOREIGN KEY (end_to_end_id, decision_seq, decided_at) REFERENCES decisions
);
CREATE INDEX IF NOT EXISTS alerts_open_idx ON alerts (raised_at DESC) WHERE status = 'open';

CREATE TABLE IF NOT EXISTS dispositions (
  id         BIGSERIAL PRIMARY KEY,
  case_id    BIGINT NOT NULL REFERENCES cases,
  analyst    TEXT NOT NULL,
  approver   TEXT,                         -- four-eyes on blocklist/consortium effects
  action     TEXT NOT NULL,
  reason     TEXT NOT NULL,
  at         TIMESTAMPTZ NOT NULL,
  propagated JSONB
);

-- ══════════════════════════════════════════════════════════════════════
--  CONFIG AUDIT
-- ══════════════════════════════════════════════════════════════════════
CREATE TABLE IF NOT EXISTS config_changes (
  id            BIGSERIAL PRIMARY KEY,
  at            TIMESTAMPTZ NOT NULL,
  bundle        TEXT NOT NULL,      -- policy | rules | model | signal_registry
  from_version  TEXT,
  to_version    TEXT NOT NULL,
  proposed_by   TEXT NOT NULL,
  approved_by   TEXT NOT NULL CHECK (approved_by <> proposed_by),   -- four-eyes, in the schema
  diff          JSONB NOT NULL,
  prev_hash     BYTEA,
  hash          BYTEA NOT NULL      -- same chain discipline as decisions
);

-- ══════════════════════════════════════════════════════════════════════
--  LOCAL BLOCKLIST — exact confirm store (never a filter hit alone)
-- ══════════════════════════════════════════════════════════════════════
CREATE TABLE IF NOT EXISTS blocklist_entries (
  token          TEXT PRIMARY KEY,     -- e.g. sha256(creditor_account)
  account        TEXT NOT NULL,
  list           TEXT NOT NULL,        -- local | consortium | watchlist
  reason         TEXT NOT NULL,
  added_by       TEXT NOT NULL,
  approved_by    TEXT,
  added_at       TIMESTAMPTZ NOT NULL,
  reporter_count INTEGER NOT NULL DEFAULT 1
);

-- ══════════════════════════════════════════════════════════════════════
--  CONSORTIUM WIRE ENTRIES (P0: HMAC + epoch pseudonym tokens)
-- ══════════════════════════════════════════════════════════════════════
CREATE TABLE IF NOT EXISTS consortium_entries (
  id             BIGSERIAL PRIMARY KEY,
  entry_id       TEXT UNIQUE NOT NULL,      -- HMAC(reporter_key, token || case_id)
  token          TEXT NOT NULL,             -- HMAC(pepper, epoch || account)
  epoch          INTEGER NOT NULL,
  reporter_bank  TEXT NOT NULL REFERENCES participants(id),
  kind           TEXT NOT NULL DEFAULT 'creditor_account',
  threat_class   TEXT NOT NULL DEFAULT '',
  status         TEXT NOT NULL DEFAULT 'ACTIVE',  -- ACTIVE|RETRACTED|DISPUTED|EXPIRED
  confidence     REAL NOT NULL DEFAULT 1.0,
  case_id        TEXT,
  created_at     TIMESTAMPTZ NOT NULL,
  expires_at     TIMESTAMPTZ,
  wire_json      JSONB              -- the exact signed wire payload (docs/05 §4.6), returned
                                     -- verbatim by GET /v1/federation/wire/{id}
);
CREATE INDEX IF NOT EXISTS consortium_token_idx ON consortium_entries (token);
