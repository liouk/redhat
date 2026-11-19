# Saved step scripts and direct log inspection

The agent first shows the complete proposed self-contained step script, exact destination, purpose, expected evidence, and any mutations. Wait for explicit confirmation of that script before writing any script file, including a temporary draft. After confirmation, save exactly those contents, syntax-check the saved file, and always run `chmod u+x` on it; this executable-bit change is explicitly authorized without separate confirmation. The user executes the script unless execution is separately delegated. Script and log must share the exact basename, such as `001-baseline.sh` and `001-baseline.log`. Assume the user is in the active verification directory and give a short `./001-baseline.sh` invocation; the Bash shebang selects its interpreter. Do not ask the user to paste or save a large command block when the agent is authorized to prepare the script.

The user reports completion; the agent then reads the matching log directly from the shared filesystem. Check the latest timestamp, command trace, errors, and terminal status before interpreting the result. Do not ask for pasted output unless direct access is unavailable. Missing logs or a missing terminal status require investigation, not an assumption of success. Concurrent monitors have separate script/log pairs and are inspected as partial streams until stopped.

Do not source step scripts or helpers, define persistent logging functions, use startup hooks, or enable tracing in the user's interactive shell. Executing a Bash script keeps its options and variables isolated from unrelated user commands.

## Step script pattern

Resolve the actual commands and get confirmation of the full contents before saving the script. Before approval, an optional syntax check can read the draft from stdin without creating a script file. After saving, syntax-check with `bash -n ./001-baseline.sh` from the verification directory; this does not run the verification payload.

```bash
#!/usr/bin/env bash
set +x
PV_RUN_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)" || exit 1
PV_SCRIPT_NAME="${BASH_SOURCE[0]##*/}"
PV_STEP_LOG="$PV_RUN_DIR/${PV_SCRIPT_NAME%.sh}.log"

(
  set -e -o pipefail
  cd -- "$PV_RUN_DIR"
  PS4=$'\033[33m[COMMAND]\033[0m '
  set -x

  date --iso-8601=seconds
  pwd
  oc get co authentication kube-apiserver console
  oc -n openshift-oauth-apiserver get svc
) 2>&1 | tee -a "$PV_STEP_LOG"
pv_status=("${PIPESTATUS[@]}")
printf '[STATUS] commands=%s tee=%s\n' "${pv_status[0]}" "${pv_status[1]}" |
  tee -a "$PV_STEP_LOG"
pv_summary_status=$?
if (( pv_status[0] != 0 )); then exit "${pv_status[0]}"; fi
if (( pv_status[1] != 0 )); then exit "${pv_status[1]}"; fi
exit "$pv_summary_status"
```

The user is assumed to work in the verification directory; present relative invocations and filenames. The script still derives its absolute run directory and matching log from its own path for reliability, so no prior exported variables or sourced files are required. Relative paths in the payload are relative to the run directory; use explicit paths for repository operations elsewhere. A successful script exits zero. A failed payload or logger exits nonzero even when the other pipeline component succeeds.

Inside the subshell, `PS4` gives executed commands a yellow `[COMMAND]` tag, `set -x` traces them to stderr, and `2>&1` merges trace and output before `tee`. Local `set -e -o pipefail` stops on an unhandled command or pipeline failure. Capture `PIPESTATUS` immediately after the outer pipeline. Both recorded statuses and the summary logger must succeed before continuing.

Keep expected failures in explicit `if`/`else` branches. Do not put the whole subshell in an `if`, `&&`, or `||` construct that disables its `errexit` behavior. Distinguish resource NotFound from forbidden or transport errors. A monitor's intentional interruption is not a failed verification assertion; inspect the last complete evidence and termination separately.

Save sanitized snapshots with an inner `tee` so they are displayed and written under the run directory. Put redirections inside the traced subshell, not around the script invocation. Retries of an unchanged script append to its log. Any script content change requires showing the full revised script and obtaining confirmation before writing. When modifying commands after execution, keep the executed pair and create a new numbered script/log pair. Make each newly saved script executable without an additional approval request. Do not rewrite historical commands to imply different evidence was collected.

Never embed credentials in step scripts, trace expanded credentials, or print credential-bearing objects. Disable tracing locally around credential acquisition/use and emit only sanitized evidence. `set +x` does not redact stdout/stderr captured by `tee`.

## Initial bootstrap

The agent prepares the exact directory path, concise README contents, initial progress record, and bootstrap commands before requesting approval. Include creation of `000-setup.sh` and `000-setup.log`, showing the complete bootstrap script for confirmation before writing it. Confirm each later script separately before writing; bootstrap approval does not preapprove later script contents. Making the saved scripts executable needs no separate confirmation. Bootstrap or script-preparation approval does not authorize running later verification payloads or changing cluster state.

The directory must exist before `tee` opens its log. For directory creation only, buffer the tagged command/output and replay it through `tee` once the directory exists. Preserve the exact bootstrap sequence in `000-setup.sh` after directory creation; label any already executed directory-creation portion clearly rather than pretending it ran from a previously existing script. Save the remaining initialization commands before executing them. Do not race `mkdir` against an outer `tee` opening a file in the new directory, silently overwrite an existing run, or store bootstrap evidence elsewhere.

Write the approved `README.md` and `run.md` using the subshell/PS4/tee pattern and record the terminal status. Confirm the files with read-only inspection and give the user a one-time `cd` into the created directory. Thereafter assume this is their working directory, including when presenting script invocations. On older runs with logs but no scripts, preserve those artifacts as historical evidence; do not invent a missing script's execution provenance. Prepare matching scripts for all subsequent steps.
