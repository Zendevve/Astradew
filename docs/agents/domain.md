# Domain Docs

This repo uses a single-context layout: `CONTEXT.md` at the repo root and ADRs under `docs/adr/`.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root: the domain glossary and model.
- **`docs/adr/`**: read ADRs that touch the area you are about to work in.

If these files or directories do not exist, proceed silently. Do not flag their absence or suggest creating them upfront. `/domain-modeling`, reached via `/grill-with-docs` and `/improve-codebase-architecture`, creates them lazily when terms or decisions get resolved.

## Use the glossary's vocabulary

When your output names a domain concept—in an issue title, refactor proposal, hypothesis, or test name—use the term defined in `CONTEXT.md` rather than synonyms the glossary explicitly avoids.

If a needed concept is missing, reconsider whether it belongs to the project's language or note the gap for `/domain-modeling`.

## Flag ADR conflicts

If your output contradicts an existing ADR, name the ADR and explain why the decision is worth reopening rather than silently overriding it.
