// Wire types mirroring go/internal/contract/*.go. snake_case throughout — this IS the API
// contract (see decision.go's own comment). Do not rename fields to camelCase; the whole
// point is that this file stays a faithful mirror of the Go structs.

export type Action =
  | "ALLOW"
  | "ALLOW_MONITOR"
  | "STEP_UP"
  | "STEP_UP_INTERSTITIAL"
  | "HOLD"
  | "CAP"
  | "BLOCK";

export const LADDER: Action[] = [
  "ALLOW",
  "ALLOW_MONITOR",
  "STEP_UP",
  "STEP_UP_INTERSTITIAL",
  "HOLD",
];

export type DecisionKind = "LIVE" | "SHADOW" | "REPLAY" | "RESOLUTION" | "CONTROL";

// Four-state chip status — contract/status.go.
export type Status = "CLEAR" | "FIRED" | "NOT_APPLICABLE" | "NOT_EVALUATED";

export interface FeatureVector {
  values: Record<string, number | null>;
  status: Record<string, Status>;
  reason: Record<string, string>;
  staleness: Record<string, number>;
}

export interface Finding {
  signal_id: string;
  status: Status;
  score: number;
  explanation: string;
  provenance: string;
  latency_ms: number;
  version: string;
  reason_code: string;
}

export interface Decision {
  end_to_end_id: string;
  decision_seq: number;
  kind: DecisionKind;
  decided_at_ms: number;
  accepted_at_ms: number;

  action: Action;
  pre_advisory_action: Action;
  rail_fired: string;
  reason_codes: string[] | null;

  p_model: number | null;
  p_prevalence_adj: number | null;
  expected_loss_minor: number | null;
  expected_cost_minor: number | null;

  features: FeatureVector | null;
  findings: Finding[] | null;
  contributions: Record<string, number> | null;

  model_bundle_version: string;
  policy_version: string;
  rules_version: string;
  signal_registry_version: string;

  is_control: boolean;
  action_propensity: number;

  degraded: string[] | null;

  total_ms: number;
  queue_delay_ms: number;
  service_ms: number;

  decision_shard: number;
  chain_seq: number;
  prev_hash: string | null; // base64
  hash: string; // base64
  checkpoint_id: number | null;
}

export interface DecisionResponse {
  decision: Decision;
  replayed: boolean;
}

// Raw canonical event fields, as persisted alongside the decision.
export interface TransactionRecord {
  end_to_end_id?: string;
  event_ts_ms?: number;
  accepted_at_ms?: number;
  rail?: string;
  channel?: string;
  bank_instance?: string;
  debtor_account?: string;
  debtor_vpa?: string;
  creditor_account?: string;
  creditor_vpa?: string;
  creditor_ifsc?: string;
  instructed_amount_minor?: number;
  currency?: string;
  device_id?: string;
  ip?: string;
  asn?: number;
  geo_cell?: string;
  session_id?: string;
  app_version?: string;
  initiation?: string;
  remittance_info?: string;
  schema_version?: number;
  [key: string]: unknown;
}

export interface DecisionDetailResponse {
  decision: Decision;
  transaction: TransactionRecord;
}

export interface DependencyHealth {
  up: boolean;
  latency_ms: number;
  error?: string;
}

export interface HealthzResponse {
  up: boolean;
  non_degraded: boolean;
  dependencies: {
    redis: DependencyHealth;
    postgres: DependencyHealth;
  };
}

export interface LatencyResponse {
  n: number;
  p50: number;
  p90: number;
  p99: number;
  p999: number;
  max: number;
}

export interface ResilienceResponse {
  dependencies: {
    redis: DependencyHealth;
    postgres: DependencyHealth;
  };
  async_shed_total: number;
  async_queue_depth: number;
  degradation_value_cap_minor: number;
}

export interface AuditVerifyResponse {
  ok: boolean;
  n: number;
  break_at: number;
}

export interface ChaosResponse {
  action: string;
  container: string;
}

export interface GraphResponse {
  RingScore: number;
  RingSize: number;
  ComponentBankCount: number;
  HopsToCashout: number;
  DeviceSharedDegree: number;
}

// Policy bundle — shape kept loose (PascalCase per the Go json defaults for this endpoint);
// rendered mostly as raw JSON on the governance view rather than fully typed field by field.
export type PolicyBundle = Record<string, unknown>;

export interface CalibrationResponse {
  calibrator_method: string;
  calibrator_version: string;
  model_bundle: string;
  prevalence: {
    version: string;
    train_prevalence: number;
    natural_prevalence: number;
  };
  score_distribution: {
    n: number;
    buckets: string[];
    counts: number[];
  };
  reliability_diagram_available: boolean;
  reliability_diagram_note: string;
}

// SSE payload on `event: decision` from /v1/stream. Also the shape of GET
// /v1/decisions/recent's rows — the two are kept identical on purpose so Live Monitor's
// hydrate-then-tail can treat them as one stream.
export interface StreamDecisionEvent {
  end_to_end_id: string;
  decided_at_ms: number;
  debtor_account: string;
  creditor_account: string;
  amount_minor: number;
  rail: string;
  action: Action;
  total_ms: number;
  degraded: string[] | null;
  // Real-time novelty signal (feature-space k-NN + conformal p-value, go/internal/novelty).
  // Shadow only — never influences action — small p-value = more anomalous. Not evaluated
  // until the calibration reservoir has >=30 points (COLD_START).
  novelty_p_value: number;
  novelty_evaluated: boolean;
  /** Prevalence-corrected fraud probability, carried on the stream so a row can show risk
   *  without a follow-up fetch. Null when the decision never reached the model. */
  p_prevalence_adj: number | null;
  /** One-line "why", ranked by signal importance. The full explanation is behind
   *  GET /v1/decisions/{id}/explain. */
  top_reason: string;
  /** Where the payment came from: judge phone, attack campaign, background traffic,
   *  scripted scenario, or a plain API call. Derived server-side from the id prefix. */
  source: "judge" | "attack" | "ambient" | "scenario" | "api";
}

// GET /v1/alerts, POST /v1/alerts/{id}/resolve — a real open/resolved queue backed by the
// `alerts` table (problem_statement.txt's "Alert management"). Raised for every LIVE
// decision that lands anywhere other than ALLOW / ALLOW_MONITOR.
export interface AlertRow {
  id: number;
  end_to_end_id: string;
  decided_at_ms: number;
  raised_at_ms: number;
  severity: "low" | "medium" | "high" | "critical";
  status: "open" | "resolved";
  resolved_at_ms: number | null;
  action: Action;
  debtor_account: string;
  creditor_account: string;
  amount_minor: number;
  rail: string;
}

export interface AlertsResponse {
  alerts: AlertRow[] | null;
  open_count: number;
}

// POST /v1/judge/session — seeds a fresh real payer with real warm-up history (via the same
// DecideAndPersist path everything else uses), for the judge-facing Payer App.
export interface JudgeScenario {
  key: string;
  persona_name: string;
  persona_blurb: string;
  merchant_account: string;
  merchant_label: string;
  merchant_sub: string;
  merchant_initials: string;
  everyday_amount_minor: number;
  scam_label: string;
  scam_initials: string;
  sender_id: string;
  caller_number: string;
  caller_caption: string;
  headline: string;
  message_body: string;
  account_caption: string;
  scam_amount_minor: number;
  why_it_works: string;
  the_truth: string;
}

export interface JudgeSessionResponse {
  session_id: string;
  payer_account: string;
  merchant_account: string;
  merchant_label: string;
  scam_account: string;
  scam_label: string;
  // The story this run drew. Every scan gets a different one, so the copy on the phone
  // comes from here rather than being baked into the component.
  scenario: JudgeScenario;
  // Live beat the phone is on, mirrored onto the console.
  act: string;
  act_label: string;
  updated_ms: number;
  last_ref?: string;
  last_action?: string;
}

// GET /v1/decisions/recent — real persisted history (Postgres), same shape as the SSE
// stream row, used to hydrate Live Monitor on load rather than leaving it empty until the
// next decision happens to fire after the tab connects.
export interface RecentDecisionsResponse {
  rows: StreamDecisionEvent[] | null;
}

export type DemoScenario = "A" | "B" | "C" | "D" | "E" | "F" | "G" | "H";

export interface DemoStep {
  label: string;
  event?: unknown;
  decision?: Decision;
  note?: string;
}

export interface DemoRunResponse {
  scenario: DemoScenario;
  expected: string;
  steps: DemoStep[];
  passed: boolean;
}

// ── Explanation, proof and live-demo surfaces ────────────────────────────────
// GET /v1/decisions/{id}/explain. This is the "why" contract: every field is derived from
// what the decision actually recorded, so the console never has to invent a reason.

export interface Evidence {
  id: string;
  title: string;
  detail: string;
  source: "rail" | "model" | "graph" | "novelty" | "consortium" | "blocklist" | "profile" | "signal";
  severity: "critical" | "high" | "medium" | "low" | "info";
  weight: number;
  signed: number;
  value: number;
  has_value: boolean;
  family: string;
  reason?: string;
}

export interface Detector {
  id: string;
  name: string;
  kind: string;
  verdict: "FIRED" | "CLEAR" | "NOT_EVALUATED" | "NOT_APPLICABLE";
  score: number | null;
  score_label: string;
  summary: string;
  independent: boolean;
  blocking: boolean;
}

export interface Counterfactual {
  kind: string;
  question: string;
  answer: string;
}

export interface ActionCost {
  action: Action;
  expected_fraud_loss_minor: number;
  friction_minor: number;
  lost_business_minor: number;
  total_cost_minor: number;
  chosen: boolean;
}

export interface ActionThreshold {
  action: Action;
  min_p: number;
}

export interface Explanation {
  end_to_end_id: string;
  action: Action;
  action_label: string;
  pre_advisory_action: Action;
  outcome: "allowed" | "challenged" | "capped" | "blocked";
  headline: string;
  narrative: string[];
  p_model: number | null;
  p_prevalence_adj: number | null;
  risk_band: string;
  evidence: Evidence[];
  cleared: Evidence[];
  not_evaluated: Evidence[];
  detectors: Detector[];
  cost_table: ActionCost[] | null;
  thresholds: ActionThreshold[] | null;
  counterfactuals: Counterfactual[] | null;
  amount_minor: number;
  rail: string;
  decided_at_ms: number;
  total_ms: number;
  reason_codes: string[] | null;
  degraded: string[] | null;
  versions: Record<string, string>;
  tier: string;
}

export interface ReproCheck {
  name: string;
  stored: string;
  recomputed: string;
  match: boolean;
  note?: string;
}

export interface TraceStep {
  stage: number;
  name: string;
  description: string;
  inputs?: Record<string, string>;
  output: string;
  outcome: "executed" | "short_circuit" | "skipped" | "not_evaluated";
}

export interface Determinism {
  reproduced: boolean;
  chain_intact: boolean;
  checks: ReproCheck[];
  trace: TraceStep[];
  note: string;
  scorer_available: boolean;
}

export interface NarratorMeta {
  provider: string;
  model: string;
  endpoint: string;
  on_premise: boolean;
  available: boolean;
  note: string;
}

export interface ExplainResponse {
  explanation: Explanation;
  determinism: Determinism;
  narrator: NarratorMeta;
}

export interface Narrative {
  summary: string;
  reasoning: string[] | null;
  next_steps: string[] | null;
  provider: string;
  model: string;
  endpoint: string;
  on_premise: boolean;
  latency_ms: number;
  deterministic: boolean;
  note: string;
}

export interface NarrateResponse {
  narrative: Narrative;
  degraded: boolean;
}

// ── simulator ───────────────────────────────────────────────────────────────

export interface CampaignSpec {
  kind: string;
  label: string;
  description: string;
  expect: string;
  steps: number;
}

export interface CampaignProgress {
  kind: string;
  label: string;
  sent: number;
  total: number;
  challenged: number;
  allowed: number;
  running: boolean;
  started_ms: number;
  narrative: string;
  value_at_risk_minor: number;
  value_stopped_minor: number;
}

export interface SimStatus {
  traffic_running: boolean;
  traffic_tps: number;
  ambient_sent: number;
  attack_sent: number;
  campaign: CampaignProgress | null;
  campaigns_available: CampaignSpec[];
}

// ── policy studio ───────────────────────────────────────────────────────────

export interface FlipRow {
  end_to_end_id: string;
  amount_minor: number;
  rail: string;
  p_prevalence_adj: number;
  from: Action;
  to: Action;
  direction: "stricter" | "looser";
  debtor_account: string;
  creditor_account: string;
}

export interface PolicyTuneResponse {
  evaluated_against: number;
  flips: FlipRow[] | null;
  flips_total: number;
  stricter: number;
  looser: number;
  value_newly_challenged_minor: number;
  value_newly_released_minor: number;
  expected_cost_delta_minor: number;
  applied: boolean;
  policy_version: string;
  note: string;
}

export interface PolicyTuneRequest {
  hold_friction_minor?: number;
  step_up_friction_minor?: number;
  interstitial_friction_minor?: number;
  false_block_cost_minor?: number;
  margin_minor?: number;
  hold_stop_prob?: number;
  loss_given_fraud_upi?: number;
  degradation_value_cap_minor?: number;
  apply?: boolean;
  replay_limit?: number;
}

export interface LivePolicyResponse {
  policy: Record<string, unknown> & { version?: string };
  base_version: string;
  is_tuned: boolean;
  approved_by: string[] | null;
}

export interface ModelMetricsResponse {
  live_latency: LatencyResponse;
  model_bundle: string;
  policy_version: string;
  rules_version: string;
  tiers: Record<string, string>;
  training: Record<string, unknown> | null;
  manifest: Record<string, unknown> | null;
  calibrator: Record<string, unknown> | null;
  prevalence: Record<string, unknown> | null;
  external_validation: Record<string, unknown> | null;
}

export interface DetectorCoverage {
  detector: string;
  fired: number;
  rate: number;
  needs_labels: boolean;
}

export interface CampaignCoverage {
  kind: string;
  label: string;
  decisions: number;
  challenged: number;
  catch_rate: number;
  value_at_risk_minor: number;
  value_challenged_minor: number;
  value_catch_rate: number;
  detectors: DetectorCoverage[];
}

export interface CoverageResponse {
  campaigns: CampaignCoverage[] | null;
  note: string;
  read_the_value_rate: string;
  why_it_matters: string;
}

// ── interactive analyst chat ────────────────────────────────────────────────
// POST /v1/decisions/{id}/chat. Grounded in the same whitelisted brief the write-up uses,
// so an answer can never contain a fact the decision record does not have.

export interface ChatTurn {
  role: "user" | "assistant";
  content: string;
}

export interface ChatAnswer {
  reply: string;
  provider: string;
  model: string;
  on_premise: boolean;
  latency_ms: number;
  grounded: boolean;
  note: string;
}

export interface ChatResponse {
  answer: ChatAnswer;
  degraded: boolean;
}

// ── analytics ───────────────────────────────────────────────────────────────
// GET /v1/analytics — group-bys over decisions this instance actually made.

export interface CutRow {
  key: string;
  label: string;
  total: number;
  challenged: number;
  rate: number;
  value_minor: number;
  value_challenged_minor: number;
  value_rate: number;
  lat?: number;
  lon?: number;
}

export interface AnalyticsResponse {
  window_from_ms: number;
  window_to_ms: number;
  decisions: number;
  challenged: number;
  value_minor: number;
  value_challenged_minor: number;
  places: CutRow[];
  typologies: CutRow[];
  rails: CutRow[];
  bands: CutRow[];
  hours: CutRow[];
  signals: CutRow[];
  note: string;
}

export interface GraphTopAccount {
  account: string;
  distinct_payers: number;
  shared_devices: number;
}

export interface GraphTopResponse {
  accounts: GraphTopAccount[];
  note: string;
}

// POST /v1/admin/reset — demo affordance; see go/cmd/nazar/handlers_reset.go.
export interface ResetReport {
  decisions_deleted: number;
  transactions_deleted: number;
  alerts_deleted: number;
  redis_keys_deleted: number;
  graph_cleared: boolean;
  novelty_cleared: boolean;
  latency_cleared: boolean;
  simulator_stopped: boolean;
  judge_session_cleared: boolean;
  policy_version: string;
  audit_chain: string;
  note: string;
  took_ms: number;
}
