import { HashRouter, Route, Routes } from "react-router-dom";
import { Shell } from "./components/Shell";
import { LiveMonitor } from "./screens/LiveMonitor";
import { Investigation } from "./screens/Investigation";
import { Resilience } from "./screens/Resilience";
import { AuditChain } from "./screens/AuditChain";
import { DemoRunner } from "./screens/DemoRunner";
import { GraphView } from "./screens/GraphView";
import { Calibration } from "./screens/Calibration";
import { LatencyView } from "./screens/LatencyView";
import { PayerAppQR } from "./screens/PayerAppQR";
import { PayerApp } from "./screens/PayerApp";
import { AnomalyDetection } from "./screens/AnomalyDetection";
import { Alerts } from "./screens/Alerts";
import { CommandCentre } from "./screens/CommandCentre";
import { PolicyStudio } from "./screens/PolicyStudio";
import { Analytics } from "./screens/Analytics";
import { ModelEvidence } from "./screens/ModelEvidence";

function App() {
  return (
    <HashRouter>
      <Routes>
        {/* Standalone, full-screen — no admin sidebar/topbar. This is the real thing a judge's
            phone lands on after scanning the QR shown on /payer-app. */}
        <Route path="pay" element={<PayerApp />} />
        <Route element={<Shell />}>
          <Route index element={<CommandCentre />} />
          <Route path="feed" element={<LiveMonitor />} />
          <Route path="policy-studio" element={<PolicyStudio />} />
          <Route path="analytics" element={<Analytics />} />
          <Route path="model" element={<ModelEvidence />} />
          <Route path="anomaly" element={<AnomalyDetection />} />
          <Route path="alerts" element={<Alerts />} />
          <Route path="investigate" element={<Investigation />} />
          <Route path="resilience" element={<Resilience />} />
          <Route path="audit" element={<AuditChain />} />
          <Route path="demo" element={<DemoRunner />} />
          <Route path="time-machine" element={<Investigation timeMachine />} />
          <Route path="graph" element={<GraphView />} />
          <Route path="calibration" element={<Calibration />} />
          <Route path="latency" element={<LatencyView />} />
          <Route path="payer-app" element={<PayerAppQR />} />
        </Route>
      </Routes>
    </HashRouter>
  );
}

export default App;
