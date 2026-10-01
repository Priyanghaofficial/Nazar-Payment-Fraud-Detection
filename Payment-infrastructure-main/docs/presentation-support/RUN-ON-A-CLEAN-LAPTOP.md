# Running Nazar on a clean laptop

Two routes. **Take route A.** Route B exists only if Docker cannot be installed.

The repo carries the trained model bundle (376 KB), so there is **no training step and no
dataset download**. Datasets are deliberately not in git.

---

## Route A — Docker Desktop (recommended, Windows/macOS/Linux)

One command brings up Redis, Postgres, the engine and the console behind nginx.

### 1. Install
- **Docker Desktop** — https://www.docker.com/products/docker-desktop
  On Windows accept the WSL 2 prompt, then reboot.
- **Git** — https://git-scm.com/download/win

### 2. Run
Open **PowerShell**:

```powershell
git clone https://github.com/NagaNeeraj-18/Payment-infrastructure.git
cd Payment-infrastructure
copy .env.local.example .env
notepad .env            # paste the Groq keys, save, close
docker compose up -d --build
```

First build takes 3–5 minutes. After that, start-up is seconds.

### 3. Open
**http://localhost** — that is the whole demo.

### 4. Phone / QR
The judge's phone must reach the laptop. Find the LAN IP:

```powershell
ipconfig | findstr IPv4
```

Open `http://<that-ip>/` on the laptop, and the QR on the Payer App screen will encode a URL
the phone can actually reach. Both devices must be on the same Wi-Fi.

If Windows Firewall prompts on first run, **allow on private networks** — otherwise the phone
cannot connect.

### 5. Stop / reset

```powershell
docker compose down            # stop, keep data
docker compose down -v         # stop and wipe Postgres (fresh demo)
```

---

## Route B — no Docker

Needs Go 1.26+, Node 22+, and Redis + Postgres 16 running locally.

```powershell
git clone https://github.com/NagaNeeraj-18/Payment-infrastructure.git
cd Payment-infrastructure

# terminal 1 — engine
$env:NAZAR_REPO_ROOT = (Get-Location).Path
$env:POSTGRES_DSN    = "postgres://nazar:nazar@localhost:5432/nazar?sslmode=disable"
$env:REDIS_ADDR      = "localhost:6379"
$env:NAZAR_LLM_API_KEYS = "gsk_...,gsk_..."
cd go; go run ./cmd/nazar

# terminal 2 — console
cd console; npm install; npm run dev
```

Apply `sql/migrations/*.sql` to the database in filename order first. Console is on
http://localhost:5173 and talks to the engine on port 8080.

---

## Pre-demo checklist

1. `docker compose ps` — four services `running`
2. http://localhost loads Command Centre, badge says **LIVE**
3. Click **Start background traffic** — rows appear
4. Click **APP scam wave** — rows turn red
5. Click a red row → panel opens → **Ask about it** → type a question, get an answer
   (proves the Groq keys are live; if it says "language model unavailable", `.env` is wrong)
6. Phone scans the QR → story loads → Command Centre shows **PHONE CONNECTED**

Do steps 1–6 **before** the judges are in the room.

---

## If something breaks mid-demo

| Symptom | Cause | Fix |
|---|---|---|
| Console loads, no data | engine not up | `docker compose logs nazar` |
| "language model unavailable" | keys missing/exhausted | demo still works — the deterministic write-up is the designed fallback, say so |
| Phone can't load the QR | firewall or wrong network | allow private networks; check both on same Wi-Fi |
| Feed frozen | SSE dropped | refresh the page; the stream reconnects |
| Port 80 in use | IIS or another server | `CONSOLE_PORT=8088 docker compose up -d`, use http://localhost:8088 |

Nothing in the demo is pre-recorded. If a detector misses something, the screen says it
missed it — that is deliberate, and it is the strongest thing you can show a judge.
