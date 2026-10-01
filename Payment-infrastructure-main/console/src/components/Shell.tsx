import { type ReactNode, useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { ThemeToggle } from "./ThemeToggle";

const ICONS: Record<string, ReactNode> = {
  live: (
    <path d="M3 12h4l3 8 4-16 3 8h4" />
  ),
  inv: (
    <path d="M4 4h6l2 3h8v13H4z" />
  ),
  res: (
    <path d="M12 3l8 3v6c0 5-3.4 8.2-8 9-4.6-.8-8-4-8-9V6z" />
  ),
  aud: (
    <>
      <path d="M10 13a5 5 0 007 0l3-3a5 5 0 00-7-7l-1 1" />
      <path d="M14 11a5 5 0 00-7 0l-3 3a5 5 0 007 7l1-1" />
    </>
  ),
  demo: <path d="M6 4l14 8-14 8z" />,
  tm: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </>
  ),
  ring: (
    <>
      <circle cx="6" cy="6" r="2.5" />
      <circle cx="18" cy="10" r="2.5" />
      <circle cx="9" cy="18" r="2.5" />
      <path d="M8.2 7.2l7.6 2M16.6 12l-6 4.4" />
    </>
  ),
  cal: <path d="M4 20V9M10 20V4M16 20v-7M22 20v-4" />,
  lat: (
    <>
      <circle cx="12" cy="13" r="8" />
      <path d="M12 13l4-3M9 2h6" />
    </>
  ),
  pay: (
    <>
      <rect x="6" y="2" width="12" height="20" rx="3" />
      <path d="M11 18h2" />
    </>
  ),
  anom: (
    <>
      <circle cx="6" cy="18" r="2" />
      <circle cx="12" cy="6" r="2" />
      <circle cx="18" cy="15" r="2" />
      <circle cx="10" cy="17" r="1.4" />
      <path d="M12 8v6M14.3 14.8l2-1.2" strokeDasharray="2 2" />
    </>
  ),
  alert: (
    <>
      <path d="M12 3l9 17H3z" />
      <path d="M12 10v4M12 17h.01" />
    </>
  ),
};

function NavIcon({ id }: { id: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      {ICONS[id]}
    </svg>
  );
}

const GROUPS: { label: string; items: { to: string; id: string; label: string }[] }[] = [
  {
    label: "Operate",
    items: [
      { to: "/", id: "live", label: "Command Centre" },
      { to: "/feed", id: "live", label: "Live Monitor" },
      { to: "/anomaly", id: "anom", label: "Anomaly Detection" },
      { to: "/alerts", id: "alert", label: "Alerts" },
      { to: "/investigate", id: "inv", label: "Investigation" },
      { to: "/resilience", id: "res", label: "Resilience" },
      { to: "/audit", id: "aud", label: "Audit Chain" },
    ],
  },
  {
    label: "Verify",
    items: [
      { to: "/analytics", id: "cal", label: "Fraud Analytics" },
      { to: "/demo", id: "demo", label: "Demo Runner" },
      { to: "/time-machine", id: "tm", label: "Time Machine" },
      { to: "/graph", id: "ring", label: "Graph / Ring" },
    ],
  },
  {
    label: "Govern",
    items: [
      { to: "/policy-studio", id: "cal", label: "Policy Studio" },
      { to: "/model", id: "tm", label: "Model Evidence" },
      { to: "/calibration", id: "cal", label: "Calibration" },
      { to: "/latency", id: "lat", label: "Latency" },
    ],
  },
  {
    label: "Customer",
    items: [{ to: "/payer-app", id: "pay", label: "Payer App" }],
  },
];

export function Shell(): ReactNode {
  const navigate = useNavigate();
  const [search, setSearch] = useState("");

  function onSearchKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter" && search.trim()) {
      navigate(`/investigate?id=${encodeURIComponent(search.trim())}`);
    }
  }

  return (
    <div className="app">
      <aside className="side">
        <div className="brand">
          <div className="lg">
            <svg viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z" />
              <circle cx="12" cy="12" r="2.6" />
            </svg>
          </div>
          <div>
            <b>Nazar</b>
            <em>P0 prototype · real backend</em>
          </div>
        </div>
        {GROUPS.map((group) => (
          <div key={group.label}>
            <div className="ng">{group.label}</div>
            {group.items.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.to === "/"}
                className={({ isActive }) => `nav ${isActive ? "on" : ""}`}
              >
                <NavIcon id={item.id} />
                {item.label}
              </NavLink>
            ))}
          </div>
        ))}
      </aside>

      <div>
        <div className="top">
          <div className="search">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
              <circle cx="11" cy="11" r="7" />
              <path d="M20 20l-3.5-3.5" />
            </svg>
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={onSearchKeyDown}
              placeholder="Look up a decision by end-to-end ID, press Enter"
              style={{ background: "transparent", border: "none", outline: "none", color: "inherit", font: "inherit", flex: 1 }}
            />
          </div>
          <div className="tr">
            <ThemeToggle />
            <div className="av">OP</div>
          </div>
        </div>
        <div className="page">
          <Outlet />
        </div>
      </div>
    </div>
  );
}
