# Codex Catalog Auto-Generation Design

## Goal

After the router starts, collect the Responses model catalog and atomically generate a Codex-compatible catalog file. Always write the OneLLM copy and, by default, also overwrite the Codex catalog file.

## Configuration

Add:

```yaml
codex:
  overwrite_catalog: true
```

The default is `true`. Setting it to `false` still generates `~/.onellm/model-catalog.json` but does not write `~/.codex/model-catalog.json`.

## Behavior

- Start model collection asynchronously after the HTTP server starts listening.
- Collect only providers supporting the Responses endpoint.
- Preserve the existing rule that configured models are authoritative; otherwise discover from the upstream Responses catalog.
- Preserve existing files when any required provider discovery fails or the resulting catalog is empty, so a transient source failure cannot replace a complete catalog with a partial one.
- Generate identical `{"models":[...]}` bytes for the HTTP `/openai/models` response and both catalog files.
- Do not modify Codex `config.toml` or `models_cache.json`.

## Paths

- OneLLM catalog: `~/.onellm/model-catalog.json`
- Codex catalog: `~/.codex/model-catalog.json`

## Reliability

Write through a temporary file in the destination directory, flush and close it, then atomically rename it over the destination. A failed generation or write is logged without stopping the router.

## Testing

- Configuration defaults to overwrite enabled and accepts explicit disablement.
- Catalog rendering remains identical between HTTP and file output.
- Overwrite enabled writes both files; disabled writes only the OneLLM file.
- Empty results preserve existing files.
- Responses endpoint filtering excludes non-Responses providers.
