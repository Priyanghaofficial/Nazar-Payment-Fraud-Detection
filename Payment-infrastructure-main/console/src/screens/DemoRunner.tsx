import { useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { DemoRunResponse, DemoScenario } from "../api/types";
import { ActionTag } from "../components/ActionTag";
import { formatMinor } from "../lib/format";

type ScenarioMeta = {
  id: DemoScenario;
  name: string;
  blurb: string;
  iconBg: string;
  iconColor: string;
  icon: ReactNode;
  pillClass: string;
  pillLabel: string;
};

const SCENARIOS: ScenarioMeta[] = [
  {
    id: "A",
    name: "Normal",
    blurb: "An ordinary transaction between two established accounts.",
    iconBg: "var(--ok-w)",
    iconColor: "var(--ok)",
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
        <circle cx="12" cy="12" r="9" />
        <path d="M8 12.5l3 3 5-6" />
      </svg>
    ),
    pillClass: "s-ok",
    pillLabel: "Allow",
  },
  {
    id: "B",
    name: "APP scam",
    blurb: "New beneficiary with an APP-scam pattern → step-up interstitial.",
    iconBg: "var(--warn-w)",
    iconColor: "var(--warn)",
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
        <path d="M12 3l9 17H3z" />
        <path d="M12 10v4M12 17h.01" />
      </svg>
    ),
    pillClass: "s-wn",
    pillLabel: "Step-up",
  },
  {
    id: "C",
    name: "Mule fan-out",
    blurb: "Fan-out into one beneficiary that forwards → graph ring evidence.",
    iconBg: "var(--teal-w)",
    iconColor: "var(--teal)",
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
        <circle cx="6" cy="6" r="2.5" />
        <circle cx="18" cy="10" r="2.5" />
        <circle cx="9" cy="18" r="2.5" />
        <path d="M8.2 7.2l7.6 2M16.6 12l-6 4.4" />
      </svg>
    ),
    pillClass: "s-hd",
    pillLabel: "Ring evidence",
  },
  {
    id: "D",
    name: "Large new payee",
    blurb: "High-value payment to a first-time beneficiary, above the ceiling.",
    iconBg: "var(--hover)",
    iconColor: "var(--ink3)",
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
        <path d="M4 8h16M4 16h16" />
      </svg>
    ),
    pillClass: "s-nt",
    pillLabel: "Cap",
  },
  {
    id: "E",
    name: "Redis killed",
    blurb: "The store dies mid-flight. The system still answers and never blocks.",
    iconBg: "var(--coral-w)",
    iconColor: "var(--coral)",
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
        <path d="M13 2L4 14h7l-1 8 9-12h-7z" />
      </svg>
    ),
    pillClass: "s-nt",
    pillLabel: "Degraded",
  },
  {
    id: "F",
    name: "Legit merchant",
    blurb: "Thirty payers to one genuine merchant. Ring score must stay zero.",
    iconBg: "var(--ok-w)",
    iconColor: "var(--ok)",
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
        <path d="M3 21V8l9-5 9 5v13" />
        <path d="M9 21v-7h6v7" />
      </svg>
    ),
    pillClass: "s-ok",
    pillLabel: "ring_score 0",
  },
  {
    id: "G",
    name: "Round-trip",
    blurb: "A persisted decision is re-read and compared byte-for-byte.",
    iconBg: "var(--indigo-w)",
    iconColor: "var(--indigo)",
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
        <path d="M21 12a9 9 0 11-3-6.7" />
        <path d="M21 3v6h-6" />
      </svg>
    ),
    pillClass: "s-ok",
    pillLabel: "Byte-identical",
  },
  {
    id: "H",
    name: "Audit chain",
    blurb: "Walks the hash chain from genesis and verifies every link.",
    iconBg: "var(--indigo-w)",
    iconColor: "var(--indigo)",
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
        <path d="M10 13a5 5 0 007 0l3-3a5 5 0 00-7-7l-1 1" />
        <path d="M14 11a5 5 0 00-7 0l-3 3a5 5 0 007 7l1-1" />
      </svg>
    ),
    pillClass: "s-ok",
    pillLabel: "Verified",
  },
];

export function DemoRunner() {
  const [results, setResults] = useState<Record<DemoScenario, DemoRunResponse | undefined>>(
    {} as Record<DemoScenario, DemoRunResponse | undefined>,
  );
  const [errors, setErrors] = useState<Record<DemoScenario, string | undefined>>(
    {} as Record<DemoScenario, string | undefined>,
  );
  const [running, setRunning] = useState<DemoScenario | null>(null);

  async function run(id: DemoScenario) {
    setRunning(id);
    setErrors((prev) => ({ ...prev, [id]: undefined }));
    try {
      const res = await api.runDemo(id);
      setResults((prev) => ({ ...prev, [id]: res }));
    } catch (e) {
      setErrors((prev) => ({ ...prev, [id]: e instanceof Error ? e.message : String(e) }));
    } finally {
      setRunning(null);
    }
  }

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Demo Runner</h1>
          <p>
            Each scenario fires through the real decision path via{" "}
            <span className="mn">POST /v1/demo/run/&#123;X&#125;</span>. Scenario E actually kills and restores
            Redis.
          </p>
        </div>
      </div>

      <div className="row r4">
        {SCENARIOS.map((s) => {
          const res = results[s.id];
          const err = errors[s.id];
          const lastDecisionStep = res ? [...res.steps].reverse().find((st) => st.decision) : undefined;
          return (
            <div className="card sc" key={s.id}>
              <div className="ic" style={{ background: s.iconBg, color: s.iconColor }}>
                {s.icon}
              </div>
              <h3>
                <u>{s.id}</u>
                {s.name}
              </h3>
              <p>{s.blurb}</p>
              <div className="ft">
                <span className={`st ${s.pillClass}`}>
                  <i />
                  {s.pillLabel}
                </span>
                <div className="sp" />
                <button className="pill sm" disabled={running !== null} onClick={() => run(s.id)}>
                  {running === s.id ? "Running…" : "Run"}
                </button>
              </div>

              {err && (
                <div className="deg" style={{ margin: "12px 0 0" }}>
                  POST /v1/demo/run/{s.id} failed: {err}
                </div>
              )}

              {res && (
                <div className="offl" style={{ flexDirection: "column", alignItems: "stretch" }}>
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8 }}>
                    <span className="sub">expected: {res.expected}</span>
                    <span className={`st ${res.passed ? "s-ok" : "s-sp"}`}>
                      <i />
                      {res.passed ? "Passed" : "Failed"}
                    </span>
                  </div>
                  <div style={{ display: "flex", flexDirection: "column", gap: 7, maxHeight: 220, overflowY: "auto", marginTop: 10 }}>
                    {res.steps.map((step, i) => (
                      <div key={`${s.id}-${i}`} style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12.5 }}>
                        <span className="mn" style={{ color: "var(--ink3)", flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                          {step.label}
                        </span>
                        {step.event ? (
                          <span className="mn" style={{ color: "var(--ink4)" }}>
                            {formatMinor(
                              (step.event as { instructed_amount_minor?: number }).instructed_amount_minor ?? null,
                            )}
                          </span>
                        ) : null}
                        {step.decision ? <ActionTag action={step.decision.action} /> : <span className="sub">{step.note ?? "—"}</span>}
                      </div>
                    ))}
                  </div>
                  {lastDecisionStep?.decision && (
                    <Link
                      to={`/investigate?id=${encodeURIComponent(lastDecisionStep.decision.end_to_end_id)}`}
                      className="sub"
                      style={{ color: "var(--indigo)", marginTop: 10, display: "inline-block" }}
                    >
                      Open in Investigation →
                    </Link>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
