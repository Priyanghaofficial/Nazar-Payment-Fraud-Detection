// Thin fetch client for the real Nazar Go backend. No mocking, no fallback data — every
// function either resolves with real backend JSON or throws, and callers render the error.
import type {
  AlertsResponse,
  AnalyticsResponse,
  ChatResponse,
  ChatTurn,
  ExplainResponse,
  LivePolicyResponse,
  CoverageResponse,
  ModelMetricsResponse,
  NarrateResponse,
  PolicyTuneRequest,
  PolicyTuneResponse,
  SimStatus,
  AuditVerifyResponse,
  CalibrationResponse,
  ChaosResponse,
  DecisionDetailResponse,
  DecisionResponse,
  DemoRunResponse,
  DemoScenario,
  GraphResponse,
  GraphTopResponse,
  HealthzResponse,
  JudgeSessionResponse,
  LatencyResponse,
  PolicyBundle,
  RecentDecisionsResponse,
  ResetReport,
  ResilienceResponse,
} from "./types";

// Resolves to whatever host served this page, port 8080 — so a phone on the same network as
// the console (e.g. scanning the judge-demo QR) reaches the real backend without hardcoding
// "localhost", which only ever means the phone itself. Falls back to localhost for anything
// not served over http/https (e.g. a file:// preview).
export const API_BASE = (() => {
  // A deployment that puts a reverse proxy in front of both the console and the engine sets
  // VITE_API_BASE="" at build time: every call then goes to the same origin, which is what
  // makes one port, one certificate and no CORS preflight possible. Any other value is used
  // verbatim. Unset (the dev default) keeps the split-port behaviour below.
  const configured = import.meta.env.VITE_API_BASE;
  if (typeof configured === "string") return configured;
  if (typeof window === "undefined" || !window.location.hostname) return "http://localhost:8080";
  const proto = window.location.protocol === "https:" ? "https:" : "http:";
  return `${proto}//${window.location.hostname}:8080`;
})();

/** Carries the HTTP status so callers can tell "not there yet" from "not there". */
export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`);
  if (!res.ok) {
    throw new ApiError(`GET ${path} -> ${res.status} ${res.statusText}`, res.status);
  }
  return res.json() as Promise<T>;
}

async function postJSON<T>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: "POST",
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    throw new ApiError(`POST ${path} -> ${res.status} ${res.statusText}`, res.status);
  }
  return res.json() as Promise<T>;
}

export const api = {
  healthz: () => getJSON<HealthzResponse>("/healthz"),
  decide: (event: unknown) => postJSON<DecisionResponse>("/v1/decide", event),
  getDecision: (endToEndId: string) =>
    getJSON<DecisionDetailResponse>(`/v1/decisions/${encodeURIComponent(endToEndId)}`),
  latency: () => getJSON<LatencyResponse>("/v1/latency"),
  resilience: () => getJSON<ResilienceResponse>("/v1/resilience"),
  auditVerify: () => getJSON<AuditVerifyResponse>("/v1/audit/verify"),
  chaosRedis: (action: "kill" | "restore") =>
    postJSON<ChaosResponse>("/v1/admin/chaos/redis", { action }),
  graphTop: () => getJSON<GraphTopResponse>("/v1/graph/top"),
  graph: (account: string) => getJSON<GraphResponse>(`/v1/graph/${encodeURIComponent(account)}`),
  policy: () => getJSON<PolicyBundle>("/v1/policy"),
  calibration: () => getJSON<CalibrationResponse>("/v1/calibration"),
  runDemo: (scenario: DemoScenario) =>
    postJSON<DemoRunResponse>(`/v1/demo/run/${scenario}`),
  recentDecisions: (limit = 100) =>
    getJSON<RecentDecisionsResponse>(`/v1/decisions/recent?limit=${limit}`),
  judgeSession: () => postJSON<JudgeSessionResponse>("/v1/judge/session"),
  judgeSessionActive: () => getJSON<{ session: JudgeSessionResponse | null }>("/v1/judge/session"),
  // Presentation state only — reports which beat the phone is on so the big screen can
  // mirror it. Fire-and-forget: a failure here must never interrupt the story.
  judgeAct: (body: { session_id: string; act: string; ref?: string; action?: string }) =>
    postJSON<{ ok: boolean }>("/v1/judge/act", body).catch(() => ({ ok: false })),
  alerts: (status: "open" | "resolved" | "all" = "open", limit = 200) =>
    getJSON<AlertsResponse>(`/v1/alerts?status=${status}&limit=${limit}`),
  resolveAlert: (id: number) => postJSON<{ status: string }>(`/v1/alerts/${id}/resolve`, { resolved_by: "operator" }),
  // Explanation & proof
  explain: (id: string) => getJSON<ExplainResponse>(`/v1/decisions/${encodeURIComponent(id)}/explain`),
  // Decisions reach Postgres on an async shipper, so /explain 404s for a moment after the
  // decision itself has returned. A judge clicking the newest row in the feed, or tapping
  // "why?" on the phone, hits exactly that window — a single attempt shows them an error for
  // a decision that is about to exist. Retry ONLY on 404, and only for as long as the drain
  // could plausibly take; every other status is a real failure and surfaces immediately.
  explainWhenReady: async (id: string, timeoutMs = 8000): Promise<ExplainResponse> => {
    const deadline = Date.now() + timeoutMs;
    let delay = 120;
    for (;;) {
      try {
        return await getJSON<ExplainResponse>(`/v1/decisions/${encodeURIComponent(id)}/explain`);
      } catch (e) {
        const notYet = e instanceof ApiError && e.status === 404;
        if (!notYet || Date.now() + delay >= deadline) throw e;
        await new Promise((r) => setTimeout(r, delay));
        delay = Math.min(delay * 2, 1000);
      }
    }
  },
  chat: (id: string, messages: ChatTurn[]) =>
    postJSON<ChatResponse>(`/v1/decisions/${encodeURIComponent(id)}/chat`, { messages }),
  narrate: (id: string) => postJSON<NarrateResponse>(`/v1/decisions/${encodeURIComponent(id)}/narrate`),
  // Live traffic & attack campaigns
  simStatus: () => getJSON<SimStatus>("/v1/sim/status"),
  simTraffic: (action: "start" | "stop", tps?: number) =>
    postJSON<{ status: string; tps?: number }>("/v1/sim/traffic", { action, tps }),
  simAttack: (kind: string) => postJSON<{ status: string }>(`/v1/sim/attack/${kind}`),
  simAttackStop: () => postJSON<{ status: string }>("/v1/sim/attack/stop"),
  // Policy studio
  livePolicy: () => getJSON<LivePolicyResponse>("/v1/policy/live"),
  tunePolicy: (req: PolicyTuneRequest) => postJSON<PolicyTuneResponse>("/v1/policy/tune", req),
  resetPolicy: () => postJSON<{ status: string; policy_version: string }>("/v1/policy/reset"),
  // Model evidence
  adminReset: () => postJSON<ResetReport>("/v1/admin/reset"),
  analytics: () => getJSON<AnalyticsResponse>("/v1/analytics"),
  modelMetrics: () => getJSON<ModelMetricsResponse>("/v1/model/metrics"),
  modelCoverage: () => getJSON<CoverageResponse>("/v1/model/coverage"),
  streamUrl: () => `${API_BASE}/v1/stream`,
};
