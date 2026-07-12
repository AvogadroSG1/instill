# Catalog-authoritative MCP dependencies

## Status

Accepted

## Context

The Library MCP Server catalog contains transport and connection details for self-defined servers. Instill previously omitted transport and registry ownership when writing APM dependencies, causing APM to treat those names as public registry references.

Existing manifests may already contain this incomplete shape. Instill also needs to preserve legitimate public registry dependencies that are not owned by the configured Library.

## Decision Drivers

- Catalog-defined MCP Servers MUST install without a public registry lookup.
- Existing incomplete catalog-backed dependencies MUST be repaired automatically.
- Legitimate unmatched registry dependencies MUST remain unchanged.
- The Library catalog MUST remain the authoritative connection definition for Library-owned MCP Servers.

## Considered Options

### Option A: Repair dependencies matched by Library catalog name

New picks and sync reconciliation use catalog transport and connection fields and explicitly set `registry: false`. Unmatched dependencies round-trip unchanged.

### Option B: Mark every object-form MCP dependency as self-defined

This is mechanically simple but can corrupt registry-backed dependencies that use object form for version or package settings.

### Option C: Retry after parsing an APM registry failure

This avoids proactive reconciliation but couples Instill to external error wording and makes a failed lookup part of the expected workflow.

## Decision Outcome

Chosen option: **Option A: repair dependencies matched by Library catalog name**.

Instill MUST treat a catalog name match as the ownership boundary. It MUST copy the catalog definition and emit an explicit non-registry dependency. It MUST preserve unmatched dependencies.

## Consequences

- `instill sync` gains a deterministic manifest-reconciliation step before APM execution.
- Catalog changes intentionally update matching project dependencies on the next sync.
- A renamed catalog entry does not silently claim or rewrite the old manifest entry.
- Tests MUST cover new selection, existing-manifest repair, and unmatched dependency preservation.

## Confirmation

BDD tests will confirm the serialized APM shape and sync reconciliation behavior. Focused package tests and the full Go suite MUST pass before completion.
