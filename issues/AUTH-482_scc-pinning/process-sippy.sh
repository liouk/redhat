#!/usr/bin/env bash

sippy_export_file="$1"

echo "| # | Component | Namespace | # Runs | # Successes | # Flakes | # Failures |"
echo "| --- | --- | --- | --- | --- | --- | --- |"
cat "$sippy_export_file" | \
  jq --raw-output 'sort_by(.current_flakes) | .[] | "| \(.jira_component) | \(.name) | \(.current_runs) | \(.current_successes) | \(.current_flakes) | \(.current_failures)"' | \
  awk '{print "| " NR " " $0}' | \
  sed -e "s/\[sig-auth\] all workloads in ns\/\([^[:space:]]*\) must set the 'openshift.io\/required-scc' annotation/\1/"
