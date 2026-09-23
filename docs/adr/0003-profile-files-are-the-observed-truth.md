# Treat a Profile's files as the observed truth

Astradew records what a Profile should contain, but the Profile's files are what SMAPI will actually load, so a disagreement between the two is surfaced and reconciled rather than overwritten. Enforcing recorded state silently discards manual work; auto-adopting every external change cannot tell intent from accident.

## Consequences

Recorded state still supplies provenance, history, and update relationships, which scanning a directory cannot recover. Restoring or deleting files is therefore never a repair strategy for a disagreement.
