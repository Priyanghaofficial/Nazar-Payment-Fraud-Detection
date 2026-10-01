.PHONY: setup dev test demo clean generate train validate-ulb console-dev console-build seed-live

CONTAINER_ENGINE ?= podman
REPO_ROOT := $(shell pwd)

# ── setup ────────────────────────────────────────────────────────────────
# Starts Redis + Postgres (rootless podman by default — no daemon required; pass
# CONTAINER_ENGINE=docker if you have a running Docker daemon instead) and applies migrations.
setup:
	@echo "==> starting Redis + Postgres via $(CONTAINER_ENGINE)"
	-$(CONTAINER_ENGINE) run -d --name nazar-redis -p 6379:6379 docker.io/redis:7-alpine redis-server --appendonly no
	-$(CONTAINER_ENGINE) run -d --name nazar-postgres -p 5432:5432 -e POSTGRES_USER=nazar -e POSTGRES_PASSWORD=nazar -e POSTGRES_DB=nazar docker.io/postgres:16-alpine
	@echo "==> waiting for postgres"
	@for i in $$(seq 1 20); do $(CONTAINER_ENGINE) exec nazar-postgres pg_isready -U nazar >/dev/null 2>&1 && break; sleep 1; done
	@echo "==> applying migrations"
	$(CONTAINER_ENGINE) exec -i nazar-postgres psql -U nazar -d nazar < sql/migrations/001_init.sql
	@echo "==> setup complete. Next: make generate && make train && make dev"

setup-restart:
	-$(CONTAINER_ENGINE) start nazar-redis nazar-postgres

# ── generator + training (optional but recommended before the first demo) ─
generate:
	cd py/generator && python3 generate.py --accounts 2000 --warmup-days 90 --out-dir ../../data/generated --seed 42

train:
	cd py/training && (test -x .venv/bin/python3 && .venv/bin/python3 train_nazar_model.py || python3 train_nazar_model.py)

validate-ulb:
	@test -f data/ulb/creditcard.arff || (mkdir -p data/ulb && curl -sL -o data/ulb/creditcard.arff "https://openml.org/data/v1/download/1673544/creditcard.arff")
	cd py/eval && python3 validate_ulb.py

# ── backend ─────────────────────────────────────────────────────────────
dev:
	cd go && NAZAR_REPO_ROOT="$(REPO_ROOT)" go run ./cmd/nazar

build:
	cd go && go build -o ../bin/nazar ./cmd/nazar

test:
	cd go && go build ./... && go vet ./... && go test ./...

# ── frontend ────────────────────────────────────────────────────────────
console-dev:
	cd console && npm install && npm run dev

console-build:
	cd console && npm install && npm run build

# ── demo ────────────────────────────────────────────────────────────────
# Runs the full A-H scripted scenario suite against a running `make dev` instance.
demo:
	@for s in A B C D E F G H; do \
		echo "=== Scenario $$s ==="; \
		curl -s -X POST http://localhost:8080/v1/demo/run/$$s | python3 -m json.tool; \
	done

seed-live:
	cd py/generator && python3 seed_live.py --url http://localhost:8080 --file ../../data/generated/demo_scenarios.jsonl

clean:
	-$(CONTAINER_ENGINE) stop nazar-redis nazar-postgres
	-$(CONTAINER_ENGINE) rm nazar-redis nazar-postgres
	rm -f data/wal.ndjson
	rm -rf bin/
