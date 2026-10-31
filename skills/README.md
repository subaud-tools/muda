# Skills

This directory holds the six skills: `muda` (entry and routing),
`muda-measure`, `muda-diagnose`, `muda-fix`, `muda-verify` and `muda-design`
(checks a planned change against the recipes, on request). The same files
are embedded in the muda binary, which installs them explicitly:

```sh
muda skills install --agent claude --scope user
```

Agents are `claude` or `pi`; scopes are `user` or `project`.

They also ship through the pi package route. The package supplies the skills,
not the binary. Each skill's preflight checks for a compatible muda binary
before use.
