# router-weighted

Router plugin that returns an ordered fallback chain.

Strategies:

- `weighted`: weighted random first choice, then weighted remaining choices.
- `shuffle`: random order.
- `ordered` / `fallback`: highest weight first.
