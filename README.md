# PharmacyCRM

PharmacyCRM is a modular monolith for operating a small pharmacy and providing a public medicine search experience.

## Repository layout

- `backend/` — independent Go application root.
- `frontend/` — independent React/TypeScript application root.
- `deploy/` — deployment manifests and operational scripts.
- `docs/` — normative project documentation and ADRs.

The two applications integrate only through the documented HTTP API; neither root may import the other application's source.

## Current implementation stage

This change establishes **E1-FND-001 — Repository roots**. Runtime services, frontend shell, Docker delivery, and business endpoints are implemented in their subsequent E1 slices.

## Checks

```sh
make architecture-check
make test
```
