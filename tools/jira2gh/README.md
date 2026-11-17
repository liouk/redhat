# jira2gh

Sync Jira-linked pull requests with a GitHub Project and update project
fields with PR metadata.

## GitHub authentication

`jira2gh` uses the GitHub CLI for all GitHub operations. Repository/PR details
and checks use normal `gh` authentication (`GH_TOKEN`, then `GITHUB_TOKEN`, then
stored credentials). Project operations use `JIRA2GH_GH_TOKEN` when nonempty,
otherwise the same normal authentication. The override is passed only to the
project's `gh` subprocess; it does not replace the process-wide token.
`assign_field.sh` also honors `JIRA2GH_GH_TOKEN`.

For public repositories and a personal project, keep public read access separate
from project writes:

```bash
export GH_TOKEN="fine-grained-token-with-public-read-access"
export GH_PROJECT_TOKEN="classic-token-with-only-project-scope"
export JIRA2GH_GH_TOKEN="$GH_PROJECT_TOKEN"
```

`GH_PROJECT_TOKEN` is a shell convenience; the tool reads `JIRA2GH_GH_TOKEN`.
An unset or empty override preserves existing `gh` authentication. Fine-grained
PATs currently cannot access user-owned Projects. These commands do not modify
PRs or repository contents. Project CLI commands may also resolve PR metadata
internally; project-only token compatibility still needs verification against
GitHub. The override does not change the token used by those internal requests.

## Install

```bash
make install
```

This installs `jira2gh` to `~/.local/bin`.

## Configuration

By default, `jira2gh` reads:

```text
~/.config/jira2gh/config.yaml
```

It still requires `JIRA_API_TOKEN` for Jira access. GitHub authentication follows the token selection described above.

```bash
export JIRA_API_TOKEN='your-jira-token'
jira2gh
```

Use `jira2gh --help` for command-line options, including `--config`,
`--project`, `--skip-jira`, and `--dry-run`.
