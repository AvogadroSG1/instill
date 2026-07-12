# MCP Schema Preservation Final Review Report

## Outcome

Status: `DONE`

The approved final-review findings were addressed at the APM manifest boundary. Library-owned MCP dependencies are repaired by exact catalog name only. Unmatched dependencies preserve custom registry selectors and uninterpreted fields during a rewrite. Catalog environment entries serialize as an APM YAML mapping and split on only the first equals sign.

## RED Evidence

Focused command:

```text
env GOCACHE=$PWD/.cache/go-build go test ./internal/instill -run 'TestAPMManifestRoundTripPreservesUnmatchedMCPDependencyFields|TestPickSerializesMCPEnvironmentAsMappingAndSplitsFirstEquals' -count=1 -v
```

Observed failures before production edits:

```text
TestAPMManifestRoundTripPreservesUnmatchedMCPDependencyFields:
error: malformed manifest: yaml: unmarshal errors:
  line 6: cannot unmarshal !!str `https:/...` into bool

TestPickSerializesMCPEnvironmentAsMappingAndSplitsFirstEquals:
serialized env was:
  - TOKEN=${TOKEN}
  - DSN=scheme://host?a=b
and did not contain the required mapping.
```

The first attempted RED command used the default macOS Go cache and failed before test execution with `operation not permitted`. Re-running with the rooted worktree cache above produced the intended behavioral RED.

## GREEN Evidence

Focused regressions:

```text
--- PASS: TestAPMManifestRoundTripPreservesUnmatchedMCPDependencyFields
--- PASS: TestPickSerializesMCPEnvironmentAsMappingAndSplitsFirstEquals
PASS
ok github.com/AvogadroSG1/instill/internal/instill
```

Affected packages:

```text
go test ./internal/instill ./internal/cli -count=1
ok github.com/AvogadroSG1/instill/internal/instill
ok github.com/AvogadroSG1/instill/internal/cli
```

Full gates:

```text
env GOCACHE=$PWD/.cache/go-build go test ./... -count=1
?  github.com/AvogadroSG1/instill [no test files]
ok github.com/AvogadroSG1/instill/internal/cli
ok github.com/AvogadroSG1/instill/internal/instill

env GOCACHE=$PWD/.cache/go-build go vet ./...
exit 0

git diff --check
exit 0
```

## Behavioral Coverage

- `registry` accepts omission, boolean values, and custom URL strings.
- Unknown MCP fields are retained through inline YAML preservation.
- A sync rewrite preserves an unmatched dependency's custom registry URL, headers, version, package, tools, and `x-owner` passthrough field.
- `local` is repaired while the differently cased `Local` remains unchanged, proving exact-name ownership and no rename inference.
- Catalog `TOKEN=${TOKEN}` and `DSN=scheme://host?a=b` entries serialize as a mapping while preserving all content after the first `=`.
- Graft import uses the same mapping adapter as Library picks and sync repair.

## Rooted Real APM Evidence

Retained root:

```text
/Users/poconnor/peter_code/scratch_work/instill_mcp_schema_preservation_code/
```

The freshly built Instill binary ran `sync` against the retained Library and project with APM CLI `0.24.1`. The install snapshot contains:

```yaml
dependencies:
  mcp:
    - name: local-stdio
      transport: stdio
      registry: false
      command: /usr/bin/true
      env:
        DSN: scheme://host?a=b
        TOKEN: ${TOKEN}
    - name: local-http
      transport: http
      registry: false
      url: https://example.test/mcp
```

The wrapper retained `install --root .../project` and `compile --root .../project` argv evidence. APM emitted no registry lookup failure and advanced to compile. Compile returned its expected fixture error, `No APM content found to compile`, because this boundary fixture intentionally contains MCP dependencies but no compilable APM instructions or agents.

Retained evidence paths:

- `library/mcp/catalog.csv`
- `project/apm.yml`
- `evidence/apm.before-real-install.yml`
- `evidence/apm.install.argv.txt`
- `evidence/apm.compile.argv.txt`
- `bin/instill`
- `bin/apm`
- `rewrite-coauthor-messages.sh`

All paths above are relative to the retained root and MUST NOT be deleted.

## Commit Metadata Verification

The approved metadata-only rewrite preserved commit order and tree IDs. Final branch range: `ddf3cb3..HEAD`.

Rewritten branch commits before this report commit:

```text
de25fe9 docs: design MCP manifest repair
871f886 docs: plan MCP manifest repair
a18fe97 fix: serialize self-defined MCP dependencies
de8a2aa fix: repair MCP dependencies during sync
49d8d24 fix: load MCP catalog during CLI sync
407c926 checkpoint: edit docs/adr/0003-catalog-authoritative-mcp-dependencies.md (+2 more)
01350ca fix: preserve complete MCP dependency schema
```

`git log ddf3cb3..HEAD --reverse --format='%(trailers:key=Co-Authored-By,valueonly)'` shows both of these separate, valid trailers on every branch commit:

```text
Peter O'Connor <poconnor@stackoverflow.com>
Codex <noreply@anthropic.com> - GPT-5
```

The corrected replacements for original commits `a491ffe`, `9c7b708`, and `a8fb339` are `de25fe9`, `871f886`, and `407c926`, respectively. Their tree IDs remain `04d086a`, `1330717`, and `b19f0f9`, confirming content preservation.

*Authored By Peter O'Connor with Assistance from Codex (GPT-5) · 2026-07-12 · MCP manifest schema preservation final review*
