# muda

muda finds where your delivery pipelines waste time, and fixes it with evidence.

*Muda* (無駄) is Japanese for "waste". The tool reads your GitHub Actions
history, shows where the minutes go, and names the waste behind them. Then it
helps an agent fix one finding at a time, in a pull request you review, and
proves the result on the real pipeline.

The loop is **measure → diagnose → fix → verify**:

1. **Measure.** Where does the delivery time go? Every number cites its runs,
   jobs and steps.
2. **Diagnose.** Which waste explains each time sink? Each finding is labelled
   `measured` or `suspected`.
3. **Fix.** One finding per pull request, approved by a person before any edit.
4. **Verify.** Before and after timings on the real pipeline, and proof that no
   gate got weaker.

## Install

The binary:

```sh
curl -fsSL https://muda.tools/install.sh | sh
```

`muda update` moves it to the latest release later.

The skills come with the binary. Install them for your agent:

```sh
muda skills install --agent claude --scope user
```

Agents are `claude` and `pi`. Scopes are `user` (the default) and
`project` (the Git root of the current repository).

The install also brings the skill of each pack you have synced, and `muda packs sync` keeps those installed copies up to date.

The skills ask you their questions on muda's page (see [The page](#the-page)),
which reaches the session through the muda plugin in Claude Code
(`/plugin install muda@subaud-tools` from the subaud-tools marketplace,
`subaud-tools/plugins`) or the muda channel in pi (channels.tools). The plugin
brings the channel, not the skills.

You can also get the skills for pi from this repository as a package:
`pi install git:github.com/subaud-tools/muda`. The package brings the skills,
not the binary. Install the binary first. Each skill checks for a compatible
muda before it starts.

### Managed setup

A release includes `muda_setup.tar.gz` beside its binaries. With kempt 0.13.0 or newer, add `setup = true` to the Muda download declaration:

```toml
[kempt]
spec = 2

[[packages.muda.download]]
site = "muda.tools"
tool = "muda"
bin = "muda"
setup = true
```

The release-bound setup installs the six built-in skills for Claude Code and pi, enables `muda@subaud-tools` in Claude Code, and registers `muda channel` in pi. It leaves unrelated settings and pi's package list alone. Existing skill files are adopted only when their front matter has both the matching `name` and `muda-owned` values; kempt 0.13.0 introduced the `adopt-markers` rule this requires.

The archive contains only the stock skills, not the reference standard, the skills README, or synced pack content. The direct `muda skills install` command and synced-pack refresh continue to manage pack skills separately.

For release builds, `muda setup-bundle --version X.Y.Z --out muda_setup.tar.gz` writes the embedded declaration and stock files without installing anything or opening the page. The publisher supplies the exact release version, including an RC suffix.

## Quickstart

muda needs a GitHub token. It uses `GH_TOKEN`, then `GITHUB_TOKEN`, then
`gh auth token`.

Most people start through their agent. In a clone of the repository, ask it to use the
`muda` skill. It confirms the baseline and the gates with you, then walks the
loop.

The commands also work on their own:

```sh
cd your-repo
muda measure --since 30d --format md   # where the time goes
muda scan --format md                  # static workflow signals
muda gates --format md                 # the gates a fix must keep
muda notices --format md               # warnings you may have missed
muda packs                             # the recipe packs this machine holds
```

`muda commands --json` lists every command. `muda help <command>` explains one.

## The page

`muda serve` opens muda's local page. On it you group repositories into a project, see what the agent found in each with its evidence, and answer there; Send takes your answers to the agent, which records them in `.muda/`. The page reaches an agent session through muda's channel: the muda plugin in Claude Code, or channels.tools in pi.

## The six skills

| Skill | What it does |
|---|---|
| `muda` | The entry point. Checks the binary, reads `.muda/`, and routes to the next step. |
| `muda-measure` | Records a baseline: roles, stacks, the gate inventory and the window, confirmed with you. |
| `muda-diagnose` | Turns the baseline into findings with evidence, an order and any exemptions. |
| `muda-fix` | Plans an approved finding's change, makes a branch and opens a pull request. |
| `muda-verify` | Checks the effect and the gate result on the pull request, and again after merge. |
| `muda-design` | On request, checks a planned pipeline or deploy change against the recipes in your packs before code is written. Its advice is expected, not measured. |

## Waste classes and recipes

The standard names ten waste classes (W1 to W10). Read it with
`muda standard` and `muda standard W4`.

Recipes are known fixes for those classes, with stack-specific detail. muda
carries none: they come in packs bought for your subaud.tools account, the ci
pack for CI and build waste first. Sign the machine in and fetch the packs it
owns:

```sh
muda login --wait
muda packs sync
```

`muda login --wait` prints a code to type on the account page and waits
until the machine is allowed. Without `--wait`, `muda login` prints the code
and exits; run it again once the machine is allowed to save the sign-in.

Then `muda recipes` lists them and `muda recipe ID --stack S` reads one.
`muda packs` shows what is cached, and `muda pack check DIR` checks a pack
folder. Without a pack, muda still measures, scans and records; a finding
naming a recipe it can't read is recorded `unchecked`.

### Submitting a recipe

Found a waste no recipe covers, or a fix or trap you measured? The diagnose
and verify skills offer to draft a recipe submission, an issue on this
repository, and post it only after you approve the text. Describe the
pattern, never your project: no organisation, repository or service names,
ids, URLs or pasted logs. Durations and ratios are what make it useful.
Accepted submissions ship in a pack.

## How proof works

- **Every number names its evidence:** a run, job, step, log line or file.
  Missing data is `unavailable`, with a reason. It is never read as zero.
- **A finding is `measured` or `suspected`.** Measured means the evidence shows
  the waste. Suspected means it is likely but not shown. A suspected finding can
  only be fixed as a labelled experiment, with pass criteria agreed first.
- **Gates are checked before and after.** `muda compare --gates` compares the
  recorded gate inventory with the pipeline after the change:
  - `VERIFIED`: the gates are the same or stronger, and that is proven.
  - `EXEMPTED`: every gap is covered by an exact exemption a person approved.
    That is acceptance, not proof.
  - `UNVERIFIED`: proof is missing. The finding is not fixed.
  - `WEAKENED`: a gate was removed or loosened. The finding is not fixed.
- **Decisions live in `.muda/`** in your repository: the baseline, findings,
  exemptions and receipts. They reach the default branch through pull requests,
  merged by you or by the agent once you say so.

## What muda never does

- It never changes repository settings, branch protection or rulesets. It only reads them.
- The fixes are made by your coding agent, guided by the skills: it edits your workflows on a branch and opens a pull request, only after you approve the plan. The `muda` binary itself only reads your pipeline and writes its record in `.muda/`.
- It merges only when you say yes to that pull request, with every required check green.

## Learn more

- [muda.tools](https://muda.tools)
- [docs/principles.md](docs/principles.md): the rules behind the standard.
- Validation receipts:
  [stage 1, a blind replay](docs/validation/2026-09-30-stage-1-replay.md) and
  [stages 2 and 3, the full loop and a public repository](docs/validation/2026-10-02-stages-2-3.md).
- [CONTRIBUTING.md](CONTRIBUTING.md)

## License

MIT. See [LICENSE](LICENSE).
