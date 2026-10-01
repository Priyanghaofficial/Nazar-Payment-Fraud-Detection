import { useEffect, useState } from "react";
import QRCode from "qrcode";

/** Admin-side launcher for the real Payer App (S0). The QR encodes this page's own origin +
 * /#/pay — whatever host actually served this console page (see api/client.ts's dynamic
 * API_BASE for the matching backend-reachability logic), so a phone on the same network
 * reaches the real thing, not a placeholder. */
const LOOPBACK_HOSTS = new Set(["localhost", "127.0.0.1", "::1"]);

export function PayerAppQR() {
  const [dataUrl, setDataUrl] = useState<string | null>(null);
  const isLoopback = LOOPBACK_HOSTS.has(window.location.hostname);
  const payUrl = `${window.location.origin}/#/pay`;

  useEffect(() => {
    let cancelled = false;
    QRCode.toDataURL(payUrl, { margin: 1, width: 260, color: { dark: "#0F172A", light: "#FFFFFF" } })
      .then((url) => {
        if (!cancelled) setDataUrl(url);
      })
      .catch(() => {
        if (!cancelled) setDataUrl(null);
      });
    return () => {
      cancelled = true;
    };
  }, [payUrl]);

  return (
    <div>
      <div className="ph">
        <div>
          <h1>Payer App</h1>
          <p>What the customer sees. Human banking language only — no rung names, no scores, no signal IDs.</p>
        </div>
        <div className="sp" />
        <a className="pill pri" href="#/pay" target="_blank" rel="noreferrer">
          Open here
        </a>
      </div>

      {isLoopback && (
        <div className="deg" style={{ marginBottom: 14 }}>
          You're viewing this console at <span className="mn">localhost</span> — a QR encoding that address will
          fail on a phone, since "localhost" there means the phone itself. Open this page from your PC's LAN
          address instead (e.g. <span className="mn">http://&lt;your-pc-ip&gt;:5173/#/payer-app</span>) and reload
          — the QR below will fix itself automatically. If the phone still can't connect once you've done that,
          your PC's firewall is almost certainly blocking the connection; on Linux with firewalld:{" "}
          <span className="mn">sudo firewall-cmd --add-port=5173/tcp --add-port=8080/tcp</span> (add{" "}
          <span className="mn">--permanent</span> to keep it past a reboot); on Windows, allow both ports through
          Windows Defender Firewall for your network's profile.
        </div>
      )}
      <div className="sp2">
        <div className="card" style={{ padding: 24, display: "flex", flexDirection: "column", alignItems: "center", gap: 14 }}>
          {dataUrl ? (
            <img src={dataUrl} width={260} height={260} alt="QR code to the payer app" style={{ borderRadius: 12 }} />
          ) : (
            <div style={{ width: 260, height: 260, display: "grid", placeItems: "center", color: "var(--ink4)" }}>
              generating…
            </div>
          )}
          <span className="mn" style={{ color: "var(--ink3)" }}>
            {payUrl}
          </span>
          <p style={{ fontSize: 12.5, color: "var(--ink3)", textAlign: "center", maxWidth: 320 }}>
            Scan with a phone on the same network. Each scan seeds a fresh real test account via{" "}
            <span className="mn">POST /v1/judge/session</span> and every tap fires the real{" "}
            <span className="mn">POST /v1/decide</span> path — nothing here is scripted client-side.
          </p>
        </div>

        <div className="card">
          <div className="ch">
            <h2>What's real here</h2>
          </div>
          <div className="meta">
            <div className="kv">
              <span className="k">Session</span>
              <span className="v">A fresh payer, warmed up with real transactions via the real decision path</span>
            </div>
            <div className="kv">
              <span className="k">Pay</span>
              <span className="v">An ordinary small payment — the real engine decides ALLOW or not</span>
            </div>
            <div className="kv">
              <span className="k">Notification</span>
              <span className="v">A scripted KYC-fee bait — the payee is genuinely new, the amount genuinely unscored until tapped</span>
            </div>
            <div className="kv">
              <span className="k">Warning</span>
              <span className="v">Shown only if the real decision actually fires — reasons are the real findings, translated to plain language</span>
            </div>
            <div className="kv">
              <span className="k">Live sync</span>
              <span className="v">Both payments land on Live Monitor and Audit Chain in real time — same backend, same instant</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
