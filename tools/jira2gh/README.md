# jira2gh

Sync Jira-linked pull requests with a GitHub Project and update project
fields with PR metadata.

## GitHub authentication

`jira2gh` uses the GitHub CLI for all GitHub operations. Authenticate `gh`
once with a personal access token; the token is stored by `gh` and does not
need to be exported globally:

```bash
printf 'GitHub PAT: ' >&2
IFS= read -r -s GH_PAT
printf '\n' >&2
printf '%s' "$GH_PAT" | gh auth login --hostname github.com --with-token
unset GH_PAT
```

For a classic personal access token, grant the `project` scope, plus `repo`
and `read:org` if the project contains private or organization repositories.
Verify the stored credentials with:

```bash
env -u GH_TOKEN -u GITHUB_TOKEN gh auth status
```

Avoid exporting an unrelated `GH_TOKEN` or `GITHUB_TOKEN`: `gh` gives those
environment variables precedence over its stored credentials.

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

It still requires `JIRA_API_TOKEN` for Jira access. GitHub authentication is
handled by `gh` as described above.

```bash
export JIRA_API_TOKEN='your-jira-token'
jira2gh
```

Use `jira2gh --help` for command-line options, including `--config`,
`--project`, `--skip-jira`, and `--dry-run`.
