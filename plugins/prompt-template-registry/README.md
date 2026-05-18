# prompt-template-registry

Static prompt registry / prompt-management plugin for Vulpes Core.

It resolves named prompt refs to versioned message templates, renders `{{variables}}`, and applies the result to the current request using a mode.

Modes:

- `replace` (default)
- `prepend_system`
- `replace_system`
- `prepend`
- `append`

Example:

```yaml
config:
  prompts:
    - name: extension-review-default
      ref: extension-review
      version: v1
      default: true
      mode: replace_system
      messages:
        - role: system
          content: >
            You are reviewing plugin risk for tenant {{tenant_id}}.
            Be concise and cite concrete evidence.
      properties:
        policy: extension-review
```

The core chooses the prompt ref from `X-Gateway-Property-Context-Key`, then `X-Gateway-Property-Prompt-Ref`, then the requested model.
