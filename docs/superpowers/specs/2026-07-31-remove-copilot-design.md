# Remove Copilot Support Design

## Goal

OneLLMRouter no longer supports GitHub Copilot. It remains a configurable
gateway for Anthropic, OpenAI Chat Completions, and OpenAI Responses providers.

## Product Boundary

- Delete GitHub device login, token storage, refresh, and Copilot API calls.
- Delete the built-in meaning of the `cp` prefix. Any prefix, including `cp`,
  is ordinary configuration and must declare a supported API endpoint.
- Delete `copilot_token` from health and desktop UI contracts.
- Preserve generic Anthropic/OpenAI translation because configured providers
  still use it.
- Delete Copilot examples, current documentation, tests, and timeout settings.
- Do not retain compatibility or migration code for `github_token`.

## Verification

- No executable source path references Copilot authentication or APIs.
- A provider without an endpoint, regardless of prefix, fails validation.
- Generic Anthropic, OpenAI, Responses, catalog, desktop, and build tests pass.
- Current configuration and user documentation contain no Copilot support.
