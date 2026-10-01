"""Typology injectors.

Each injector is a function `inject(population, start_ms, end_ms, rng, idc) -> (events, labels)`
that layers one labelled pattern on top of an already-warmed-up population. `events` are wire
events (contract-shaped dicts); `labels` never appear in the event JSON -- they are a strictly
separate side channel (CLAUDE.md: "the backend must never see ground truth").

Label shape: {"end_to_end_id", "label": bool, "typology": str, "available_at_offset_hours": float}
`available_at_offset_hours` is how long after the *event* a realistic label would actually be
known (docs/02 §7 point-in-time training-query guard, F-57) -- e.g. a card-testing burst surfaces
in minutes via issuer velocity rules; an APP-scam victim report can take weeks.
"""
