# Manifest as the ownership boundary for agent permissions

When reconcile writes agent permissions to `settings.local.json`, it only adds or removes entries for skills that are currently in the Manifest. A `Skill(name)` entry whose name does not appear in the Manifest is left untouched — instill does not own it, regardless of how it got there.

## Considered Options

**Option A (chosen): Manifest is the boundary.** instill owns the permission entry for a skill if and only if that skill is in the Manifest. Skills not in the Manifest are invisible to reconcile.

**Option B: instill owns the entire `Skill(...)` namespace.** Any `Skill(...)` entry in `settings.local.json` is treated as instill's domain; entries not matching the Manifest are removed. Simpler logic, but silently removes permissions a developer added manually for skills outside their Library.

**Option C: Explicit tracking file.** instill writes a separate sidecar recording which entries it created, and only removes those. Precise, but adds a new artifact to manage and can drift out of sync.

## Why Option A

Option A gives `settings.local.json` the same ownership contract as `.claude/skills/`: the Manifest is authoritative for what instill manages, and everything else is out of scope. A developer who manually adds `Skill(my-private-tool)` without a Manifest entry keeps that permission across reconcile runs. Option B would silently strip it; Option C is more machinery than the problem warrants.
