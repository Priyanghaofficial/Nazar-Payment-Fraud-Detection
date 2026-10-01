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

- **Backend:** Go
- **Machine Learning:** Python, LightGBM
- **Frontend:** React, TypeScript, Tailwind CSS
- **Database:** PostgreSQL
- **Real-Time Store:** Redis
- **Rules:** CEL
- **Containerization:** Docker / Podman
- **Build & Automation:** Make
- **Data Processing:** Python

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
