# Verification run

- Target PR/branch:
- Target SHA (full):
- Previous verified SHA and evidence:
- Tested image digest:
- Cluster/context and topology:
- Started at (with timezone):
- Run directory (absolute path, pair-verification-{short-sha}-{yyyymmdd}):
- Execution scope authorized by the user:

At initialization, also write a concise `README.md` in this directory with the verification aim, target PR/revision, scenarios in scope, and intended success criteria. Keep detailed progress in this file (`run.md`).

## Scope

| Scenario | Reason to repeat or carry forward | Pass criteria | Status | Evidence |
| --- | --- | --- | --- | --- |

Use statuses such as planned, in progress, passed, failed, and not run. Keep carried-forward conclusions distinguishable from newly executed checks.

## Baseline and temporary changes

Record relevant mode, feature gates, health, and original settings. Store only the configuration needed for restoration; protect any credential-bearing snapshots separately.

| Temporary change | Original value/evidence | Restoration command or procedure | Restoration status |
| --- | --- | --- | --- |

## Step 001 — Purpose

Prerequisites:

Commands (identify read-only checks and mutations):

- Script: `001-purpose.sh`
- Corresponding output: `001-purpose.log`
- Invocation: `./001-purpose.sh` (working directory: this verification directory)

Show the complete proposed script and exact path, then wait for explicit confirmation before writing it. Save the confirmed self-contained script using the pattern in `references/logging.md`, syntax-check it, and make it executable with `chmod u+x` without separate confirmation before handing it over. Ask the user to report completion; read the output log directly rather than requesting pasted output.

```bash
# Body inside this step's traced subshell; resolve the command for this run.
command arg1 arg2
```

Every traced command displays a colored `[COMMAND]` tag and appends its stdout/stderr to this step's log through `tee`. Both status values must be zero before proceeding. Save this step's manifests, patches, snapshots, and monitor output in the same run directory.

Expected evidence and condition for proceeding:

Actual result:

- Timestamp and exit code, when captured:
- Output or evidence path:
- Latest attempt and completion status confirmed by direct log inspection:
- Interpretation:
- Restoration needed:

## Continuation

- Last completed step:
- Active monitors and how to stop them:
- Current temporary changes:
- Unexpected results or unresolved questions:
- Next step and prerequisites to recheck:

## Final result

- Exact revision/image tested:
- Scenarios passed, failed, or not run:
- Previous verification carried forward:
- Evidence limits:
- Restoration confirmed or pending:
- Verification comment drafted/published, if requested:
