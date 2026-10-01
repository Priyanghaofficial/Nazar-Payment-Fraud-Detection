# Nazar – Real-Time Payment Fraud Detection

Nazar is a real-time payment fraud detection prototype designed to identify suspicious transactions quickly and support investigation through risk scoring, anomaly detection, alerts, and explainable decisions.

## Overview

The system processes payment transactions through a real-time decision engine and evaluates multiple signals before producing a decision.

The main components include:

- Real-time transaction processing
- Fraud risk scoring
- Rule-based checks
- Machine learning-based scoring
- Anomaly and novelty detection
- Graph-based fraud signals
- Alert management
- Explainable fraud decisions
- Audit-chain verification
- Real-time operator console
- Synthetic transaction generation
- Model training and validation

## Key Features

### Real-Time Decision Engine

The decision engine processes transactions through multiple stages including local filters, regulatory checks, trusted-pair handling, model scoring, expected-cost minimisation, and policy rules.

### Machine Learning

The project uses a LightGBM model trained through the Python training pipeline. The trained model is loaded by the Go decision service so that Python is not required in the transaction request path.

### Redis Profile Store

Redis is used for real-time transaction and profile information, including time-window based calculations.

### Graph-Based Fraud Detection

The system maintains an in-process graph to identify suspicious relationships between payers, merchants, and devices.

### Novelty Detection

A feature-space k-NN approach with conformal p-values is used to identify unusual transaction behaviour.

### Alerts and Investigation

Suspicious transactions can generate alerts that can be reviewed through the operator console.

### Explainability

The system provides evidence and decision information to help explain why a transaction was considered suspicious.

### Audit Chain

Important decisions are recorded using a SHA-256 hash chain. The chain can be verified to detect changes in recorded history.

### Resilience

The system includes a self-enforced decision deadline and a degraded path for handling Redis failures.

## Technology Stack

- Go
- Python
- LightGBM
- React
- TypeScript
- Tailwind CSS
- PostgreSQL
- Redis
- CEL
- Docker / Podman
- Make

## Architecture

```text
                    Payment Transaction
                           |
                           v
                  +-------------------+
                  |  Decision Engine  |
                  |       (Go)        |
                  +---------+---------+
                            |
          +-----------------+-----------------+
          |                 |                 |
          v                 v                 v
     Local Rules       Redis Profiles     ML Model
                                           (LightGBM)
          |                 |                 |
          +-----------------+-----------------+
                            |
                            v
                    Risk / Decision
                            |
              +-------------+-------------+
              |                           |
              v                           v
        Fraud Alert                 Audit Chain
              |                           |
              v                           v
      Operator Console             PostgreSQL
```

## Project Structure

```text
go/                 Decision service
py/generator/       Synthetic transaction generator
py/training/        Feature processing and LightGBM training
py/eval/            Dataset validation
console/            React/TypeScript operator console
features/           Feature registry
policy/             Decision policy bundles
rules/              CEL rule bundles
sql/migrations/     Database migrations
docs/               Architecture and project documentation
```

## Quick Start

### Requirements

- Go 1.22+
- Python 3.11+
- Node.js 20+
- Podman or Docker

### Setup

Start Redis and PostgreSQL and apply the database migrations:

```bash
make setup
```

Generate synthetic transaction data:

```bash
make generate
```

Train the LightGBM model:

```bash
make train
```

Start the backend:

```bash
make dev
```

Start the React console in another terminal:

```bash
make console-dev
```

Run the demonstration scenarios:

```bash
make demo
```

## Testing

Run the project test suite:

```bash
make test
```

The test suite includes Go build checks, static analysis, and architecture/invariant tests.

## Dataset Validation

The project also supports optional validation using the ULB credit-card fraud dataset:

```bash
make validate-ulb
```

This is used as a validation exercise for the training methodology and should not be interpreted as a direct measurement of real-world payment fraud detection performance.

## Project Objective

The objective of Nazar is to provide a production-shaped prototype for real-time payment fraud detection that can process transactions quickly, identify suspicious behaviour, provide explainable decisions, and maintain an auditable record of decisions.

## Project Type

Team Project

**Project:** Nazar – Real-Time Payment Fraud Detection System
