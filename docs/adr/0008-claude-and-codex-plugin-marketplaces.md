# Claude and Codex Plugin Marketplace Discovery

**Status:** accepted  
**Supersedes:** ADR-0004's marketplace-format requirements only

Instill accepts Claude marketplaces at `.claude-plugin/marketplace.json` and Codex repository marketplaces at `.agents/plugins/marketplace.json`. It autodetects only when exactly one index exists; repositories containing both require `--format claude|codex`, and Instill does not merge them. Codex marketplace sources must be repository-local paths; unsupported entries are skipped and reported, and the import fails if no plugins remain. Codex packages prefer root `plugin.json` and fall back to `.codex-plugin/plugin.json`; if both are present, their names must match and match the marketplace entry. A normalized GitHub repository may be registered once, and its selected format is persisted so updates refresh the same index. Existing catalog rows without a format remain Claude rows. ADR-0004's immutable SHA, path validation, mutation safety, and APM ownership requirements remain in effect.
