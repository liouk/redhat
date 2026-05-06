#!/bin/bash

set -euo pipefail

ORIGIN_MONITORTEST="${ORIGIN_MONITORTEST:-$HOME/redhat/repos/openshift/origin/pkg/monitortests/authentication/requiredsccmonitortests/monitortest.go}"
HTML_OUTPUT="scc_pinning_report.html"

# parse args
VERSIONS=()
HTML_MODE=false
for arg in "$@"; do
  if [[ "$arg" == "--html" ]]; then
    HTML_MODE=true
  else
    VERSIONS+=("$arg")
  fi
done

if [[ ${#VERSIONS[@]} -eq 0 ]]; then
  echo "Usage: $0 <version...> [--html]"
  echo "  e.g. $0 4.19"
  echo "  e.g. $0 4.19 4.20 4.21 --html"
  exit 1
fi

# extract non-runlevel namespaces from namespacesWithPendingSCCPinning in origin
PENDING_NS=$(sed -n '/namespacesWithPendingSCCPinning/,/^)/p' "$ORIGIN_MONITORTEST" \
  | grep '"openshift-' \
  | sed 's/.*"\(.*\)".*/\1/' \
  | sort)

FILTER='{"items":[{"columnField":"name","operatorValue":"contains","value":"all workloads in ns/"},{"columnField":"name","operatorValue":"ends with","value":"must set the '\''openshift.io/required-scc'\'' annotation"}],"linkOperator":"and"}'

ENCODED_FILTER=$(python3 -c "import urllib.parse; print(urllib.parse.quote('''$FILTER'''))")

PENDING_JSON=$(echo "$PENDING_NS" | jq -R . | jq -s .)

JQSCRIPT=$(mktemp)
trap 'rm -f "$JQSCRIPT"' EXIT

cat > "$JQSCRIPT" << 'EOF'
# namespaces where no code fix is needed (system ns, no workloads we control, etc.)
["default", "kube-system", "kube-public", "kube-node-lease",
 "openshift", "openshift-node", "openshift-infra",
 "openshift-apiserver", "openshift-host-network", "openshift-operators",
 "openshift-console-user-settings", "openshift-ovirt-infra",
 "openshift-cloud-platform-infra", "openshift-config", "openshift-config-managed"] as $skip |

# run-level namespaces (low priority, SCC pinning is less critical)
["openshift-cloud-controller-manager", "openshift-cloud-controller-manager-operator",
 "openshift-cluster-api", "openshift-cluster-machine-approver",
 "openshift-dns", "openshift-dns-operator",
 "openshift-etcd", "openshift-etcd-operator",
 "openshift-kube-apiserver", "openshift-kube-apiserver-operator",
 "openshift-kube-controller-manager", "openshift-kube-controller-manager-operator",
 "openshift-kube-proxy",
 "openshift-kube-scheduler", "openshift-kube-scheduler-operator",
 "openshift-multus", "openshift-network-operator",
 "openshift-ovn-kubernetes", "openshift-sdn", "openshift-storage"] as $runlevel |

# extract namespace from test name, aggregate across variants
[ .[] |
  (.name | gsub(".*all workloads in ns/"; "") | gsub(" must set the .openshift\\.io/required-scc. annotation"; "")) as $ns |
  {ns: $ns, test_name: .name, runs: .current_runs, pass: .current_successes, flakes: .current_flakes, fail: .current_failures}
] |
group_by(.ns) |
map({
  ns: .[0].ns,
  test_name: .[0].test_name,
  runs:   (map(.runs)   | add),
  pass:   (map(.pass)   | add),
  flakes: (map(.flakes) | add),
  fail:   (map(.fail)   | add)
}) |

# filter out skip and runlevel
map(select(.ns as $n | $skip | index($n) | not)) |
map(select(.ns as $n | $runlevel | index($n) | not)) |

# classify each namespace
map(. + {
  in_pending: (.ns as $n | $pending | index($n) | . != null),
  flaking: (.flakes > 0 or .fail > 0)
}) |

# still flaking — needs fix
(map(select(.flaking and .in_pending))     | sort_by(.ns)) as $known |
# flaking but NOT in pending list — new regression or missing entry
(map(select(.flaking and (.in_pending | not))) | sort_by(.ns)) as $new |
# in pending list but no longer flaking — fix landed, can remove from list
($pending | map(select(. as $n |
  ($runlevel | index($n) | . == null) and
  ($skip | index($n) | . == null)
)) | map(select(. as $n |
  ($known + $new) | map(.ns) | index($n) | . == null
)) | sort) as $fixed |

def sippy_link:
  (.test_name | @uri) as $test |
  "https://sippy.dptools.openshift.org/sippy-ng/tests/\($version)/analysis?test=\($test)";

{
  version: $version,
  no_data: false,
  known: ($known | map(. + {sippy: sippy_link})),
  new:   ($new   | map(. + {sippy: sippy_link})),
  fixed: $fixed,
  summary: {
    known: ($known | length),
    new:   ($new   | length),
    fixed: ($fixed | length)
  }
}
EOF

# collect data for all versions
echo "Fetching Sippy data..." >&2
ALL_DATA="[]"
for v in "${VERSIONS[@]}"; do
  echo "  querying v${v}..." >&2

  # check if version exists in Sippy
  VERSION_EXISTS=$(curl -s "https://sippy.dptools.openshift.org/api/tests?release=${v}&limit=1" | jq 'length')
  if [[ "$VERSION_EXISTS" -eq 0 ]]; then
    echo "    version not found in Sippy" >&2
    VDATA=$(jq -n --arg version "$v" '{version: $version, no_data: true, known: [], new: [], fixed: [], summary: {known: 0, new: 0, fixed: 0}}')
  else
    URL="https://sippy.dptools.openshift.org/api/tests?release=${v}&filter=${ENCODED_FILTER}"
    VDATA=$(curl -s "$URL" | jq --argjson pending "$PENDING_JSON" --arg version "$v" -f "$JQSCRIPT")
  fi
  ALL_DATA=$(echo "$ALL_DATA" | jq --argjson vdata "$VDATA" '. + [$vdata]')
done

# terminal output (non-HTML, single version only)
if [[ "$HTML_MODE" == false ]]; then
  DATA=$(echo "$ALL_DATA" | jq '.[0]')

  if [[ "$(echo "$DATA" | jq -r '.no_data')" == "true" ]]; then
    echo "WARNING: No test data found for version $(echo "$DATA" | jq -r '.version'). Version may not exist in Sippy."
    exit 1
  fi

  KNOWN=$(echo "$DATA" | jq -r '
    if (.known | length) > 0 then
      (["NAMESPACE", "RUNS", "PASS", "FLAKES", "FAIL", "SIPPY"] | @tsv),
      (.known[] | [.ns, (.runs|tostring), (.pass|tostring), (.flakes|tostring), (.fail|tostring), .sippy] | @tsv)
    else
      "  (none)"
    end
  ' | column -t -s $'\t')

  NEW=$(echo "$DATA" | jq -r '
    if (.new | length) > 0 then
      (["NAMESPACE", "RUNS", "PASS", "FLAKES", "FAIL", "SIPPY"] | @tsv),
      (.new[] | [.ns, (.runs|tostring), (.pass|tostring), (.flakes|tostring), (.fail|tostring), .sippy] | @tsv)
    else
      "  (none)"
    end
  ' | column -t -s $'\t')

  FIXED=$(echo "$DATA" | jq -r '
    if (.fixed | length) > 0 then .fixed[] else "  (none)" end
  ')

  echo "=== Still flaking (in pending list) ==="
  echo "$KNOWN"
  echo ""
  echo "=== New: flaking but NOT in pending list ==="
  echo "$NEW"
  echo ""
  echo "=== Fixed: in pending list but no longer flaking (can be removed) ==="
  echo "$FIXED"
  exit 0
fi

# HTML output
echo "Generating HTML report..." >&2

cat > "$HTML_OUTPUT" << 'HTMLHEAD'
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>SCC Pinning Status Report</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f5f5f5; color: #1a1a1a; padding: 24px; }
  h1 { font-size: 1.6em; margin-bottom: 4px; }
  .timestamp { color: #666; font-size: 0.85em; margin-bottom: 24px; }
  .summary { display: flex; gap: 16px; margin-bottom: 32px; flex-wrap: wrap; }
  .summary-card { background: #fff; border-radius: 8px; padding: 16px 20px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); min-width: 160px; }
  .summary-card h3 { font-size: 0.85em; color: #666; margin-bottom: 8px; }
  .summary-card .counts { display: flex; gap: 12px; font-size: 0.9em; }
  .summary-card .counts span { display: flex; align-items: center; gap: 4px; }
  .dot { width: 10px; height: 10px; border-radius: 50%; display: inline-block; }
  .dot-flaking { background: #e67e22; }
  .dot-new { background: #e74c3c; }
  .dot-fixed { background: #27ae60; }
  .version-section { background: #fff; border-radius: 8px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); margin-bottom: 24px; overflow: hidden; }
  .version-header { background: #2c3e50; color: #fff; padding: 12px 20px; font-size: 1.1em; display: flex; justify-content: space-between; align-items: center; }
  .version-header .badges span { font-size: 0.8em; padding: 2px 10px; border-radius: 12px; margin-left: 8px; }
  .badge-flaking { background: #e67e22; }
  .badge-new { background: #e74c3c; }
  .badge-fixed { background: #27ae60; }
  .section-group { padding: 16px 20px; }
  .section-group + .section-group { border-top: 1px solid #eee; }
  .section-group h3 { font-size: 0.95em; margin-bottom: 10px; color: #444; }
  table { width: 100%; border-collapse: collapse; font-size: 0.85em; }
  th { text-align: left; padding: 6px 10px; border-bottom: 2px solid #ddd; color: #666; font-weight: 600; }
  td { padding: 6px 10px; border-bottom: 1px solid #eee; }
  tr.row-flaking { background: #fef9e7; }
  tr.row-new { background: #fdedec; }
  .fixed-list { list-style: none; display: flex; flex-wrap: wrap; gap: 8px; }
  .fixed-list li { background: #eafaf1; color: #1e8449; padding: 4px 12px; border-radius: 4px; font-size: 0.85em; }
  .none-msg { color: #999; font-size: 0.85em; font-style: italic; }
  a { color: #2980b9; text-decoration: none; }
  a:hover { text-decoration: underline; }
  .num { text-align: right; font-variant-numeric: tabular-nums; }
</style>
</head>
<body>
HTMLHEAD

echo "<h1>SCC Pinning Status Report</h1>" >> "$HTML_OUTPUT"
echo "<p class=\"timestamp\">Generated: $(date '+%Y-%m-%d %H:%M:%S')</p>" >> "$HTML_OUTPUT"

# summary cards
echo '<div class="summary">' >> "$HTML_OUTPUT"
echo "$ALL_DATA" | jq -r '.[] |
  if .no_data then
    "<div class=\"summary-card\" style=\"opacity:0.5\"><h3>\(.version)</h3><div class=\"counts\"><span>no data</span></div></div>"
  else
    "<div class=\"summary-card\"><h3>\(.version)</h3><div class=\"counts\">" +
    "<span><span class=\"dot dot-flaking\"></span> \(.summary.known) flaking</span>" +
    "<span><span class=\"dot dot-new\"></span> \(.summary.new) new</span>" +
    "<span><span class=\"dot dot-fixed\"></span> \(.summary.fixed) fixed</span>" +
    "</div></div>"
  end
' >> "$HTML_OUTPUT"
echo '</div>' >> "$HTML_OUTPUT"

# per-version sections
echo "$ALL_DATA" | jq -r '.[] |
  if .no_data then
    "<div class=\"version-section\">" +
    "<div class=\"version-header\" style=\"background:#95a5a6\"><span>\(.version)</span><span class=\"badges\"><span style=\"background:#7f8c8d\">no data</span></span></div>" +
    "<div class=\"section-group\"><p class=\"none-msg\" style=\"color:#e74c3c;font-style:normal\">&#9888; No test data found for this version. It may not exist in Sippy yet.</p></div>" +
    "</div>"
  else
  "<div class=\"version-section\">" +
  "<div class=\"version-header\"><span>\(.version)</span><span class=\"badges\">" +
  (if .summary.known > 0 then "<span class=\"badge-flaking\">\(.summary.known) flaking</span>" else "" end) +
  (if .summary.new > 0 then "<span class=\"badge-new\">\(.summary.new) new</span>" else "" end) +
  (if .summary.fixed > 0 then "<span class=\"badge-fixed\">\(.summary.fixed) fixed</span>" else "" end) +
  "</span></div>" +

  # known flaking
  "<div class=\"section-group\"><h3>Still flaking (in pending list)</h3>" +
  (if (.known | length) > 0 then
    "<table><tr><th>Namespace</th><th class=\"num\">Runs</th><th class=\"num\">Pass</th><th class=\"num\">Flakes</th><th class=\"num\">Fail</th><th>Sippy</th></tr>" +
    ([.known[] |
      "<tr class=\"row-flaking\"><td>\(.ns)</td><td class=\"num\">\(.runs)</td><td class=\"num\">\(.pass)</td><td class=\"num\">\(.flakes)</td><td class=\"num\">\(.fail)</td><td><a href=\"\(.sippy)\" target=\"_blank\">view</a></td></tr>"
    ] | join("")) +
    "</table>"
  else
    "<p class=\"none-msg\">None</p>"
  end) +
  "</div>" +

  # new
  "<div class=\"section-group\"><h3>New: flaking but NOT in pending list</h3>" +
  (if (.new | length) > 0 then
    "<table><tr><th>Namespace</th><th class=\"num\">Runs</th><th class=\"num\">Pass</th><th class=\"num\">Flakes</th><th class=\"num\">Fail</th><th>Sippy</th></tr>" +
    ([.new[] |
      "<tr class=\"row-new\"><td>\(.ns)</td><td class=\"num\">\(.runs)</td><td class=\"num\">\(.pass)</td><td class=\"num\">\(.flakes)</td><td class=\"num\">\(.fail)</td><td><a href=\"\(.sippy)\" target=\"_blank\">view</a></td></tr>"
    ] | join("")) +
    "</table>"
  else
    "<p class=\"none-msg\">None</p>"
  end) +
  "</div>" +

  # fixed
  "<div class=\"section-group\"><h3>Fixed: can be removed from pending list</h3>" +
  (if (.fixed | length) > 0 then
    "<ul class=\"fixed-list\">" +
    ([.fixed[] | "<li>\(.)</li>"] | join("")) +
    "</ul>"
  else
    "<p class=\"none-msg\">None</p>"
  end) +
  "</div>" +

  "</div>"
  end
' >> "$HTML_OUTPUT"

echo "</body></html>" >> "$HTML_OUTPUT"

echo "Report written to ${HTML_OUTPUT}" >&2
xdg-open "$HTML_OUTPUT" 2>/dev/null || open "$HTML_OUTPUT" 2>/dev/null || echo "Open ${HTML_OUTPUT} in a browser" >&2
