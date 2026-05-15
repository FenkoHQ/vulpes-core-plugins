# prompt-context-injector

Strict prompt provider for model/key/tenant/subject based context injection.

The core calls this plugin when configured as `pipeline.prompt_provider`. The lookup key comes from `X-Gateway-Property-Context-Key` if present, otherwise the requested model.

Modes:

- `prepend_system`
- `replace_system`
- `prepend`
- `append`
- `replace`

Example:

```yaml
pipeline:
  prompt_provider: context-injector

plugins:
  - name: context-injector
    capabilities: [prompt_provider]
    config:
      rules:
        - name: pci-policy
          key: pci
          model: gpt-4o-mini
          mode: replace_system
          content: You are handling PCI data. Do not reveal secrets.
```
