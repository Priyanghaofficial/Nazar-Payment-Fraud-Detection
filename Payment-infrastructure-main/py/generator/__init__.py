"""Nazar synthetic data generator.

Produces well-formed canonical events (matching go/internal/contract/event.go byte-for-byte
on field names) plus a separate ground-truth label side-channel. See README below (module
docstrings) for what each file is for.

Claim-tier note (docs/03 §2.2, CLAUDE.md): any recovery rate computed by replaying this
generator's own labels back through the pipeline is [RECOVERED], never a real detection rate.
This package does not compute or print such a number itself -- it only manufactures the data.
"""
