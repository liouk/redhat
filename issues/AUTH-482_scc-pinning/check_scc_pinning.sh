#!/bin/bash

set -euo pipefail

ORIGIN_MONITORTEST_PATH="pkg/monitortests/authentication/requiredsccmonitortests/monitortest.go"
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

# fetch namespacesWithPendingSCCPinning from origin for a given version
fetch_pending_ns() {
  local version="$1"
  local branch="release-${version}"
  local url="https://raw.githubusercontent.com/openshift/origin/${branch}/${ORIGIN_MONITORTEST_PATH}"
  local content
  content=$(curl -sf "$url" 2>/dev/null) || return 1
  echo "$content" \
    | sed -n '/namespacesWithPendingSCCPinning/,/^)/p' \
    | grep '"openshift-' \
    | sed 's/.*"\(.*\)".*/\1/' \
    | sort
}

FILTER='{"items":[{"columnField":"name","operatorValue":"contains","value":"all workloads in ns/"},{"columnField":"name","operatorValue":"ends with","value":"must set the '\''openshift.io/required-scc'\'' annotation"}],"linkOperator":"and"}'

ENCODED_FILTER=$(python3 -c "import urllib.parse; print(urllib.parse.quote('''$FILTER'''))")

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
  in_pending: (.ns as $n | $pending | index($n) | . != null)
}) |

# pending namespaces: flakes > 0 means still needs fix
(map(select(.in_pending and .flakes > 0))     | sort_by(.ns)) as $known |
# non-pending namespaces: flakes > 0 OR fail > 0 (fail may be aggregator noise, filtered later)
(map(select((.in_pending | not) and (.flakes > 0 or .fail > 0))) | sort_by(.ns)) as $new |
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

# fetch test failure outputs for a given version and test name, extract unique workloads
fetch_workloads() {
  local version="$1"
  local test_name="$2"
  local encoded_test
  encoded_test=$(python3 -c "import urllib.parse, sys; print(urllib.parse.quote(sys.argv[1]))" "$test_name")

  local response http_code
  response=$(curl -sf -w "\n%{http_code}" "https://sippy.dptools.openshift.org/api/tests/outputs?release=${version}&test=${encoded_test}" 2>&1)
  local curl_exit=$?

  http_code=$(echo "$response" | tail -n1)
  response=$(echo "$response" | sed '$d')

  # return empty array if curl failed or response is not valid JSON
  if [[ $curl_exit -ne 0 ]]; then
    echo "    warning: curl failed (exit $curl_exit) for ${version}/${test_name}" >&2
    echo "[]"
    return
  fi

  if [[ "$http_code" != "200" ]]; then
    echo "    warning: HTTP ${http_code} for ${version}/${test_name}" >&2
    echo "[]"
    return
  fi

  if ! echo "$response" | jq empty 2>/dev/null; then
    echo "    warning: invalid JSON response for ${version}/${test_name}" >&2
    echo "[]"
    return
  fi

  echo "$response" | jq '[.[]? | .output | split("\n")[] | select(length > 0) |
        capture("annotation missing from pod '\''(?<pod>[^'\'']+)'\''( \\(owners: (?<owners>[^)]+)\\))?; (?<detail>.+)") |
        {owners: (.owners // .pod), detail} |
        .owners |= (split(", ") | map(
          # strip trailing k8s-generated suffixes (hashes, numbers, random strings)
          # replicaset/foo-7bf8c4d -> replicaset/foo
          # job/image-pruner-29633760 -> job/image-pruner
          # job/periodic-gathering-4jpxk -> job/periodic-gathering
          # catalogsource/oo-5bh82 -> catalogsource/oo
          # keep stripping until no more trailing random segments
          # long hex hashes (OLM bundle jobs): job/0bc98bfa3732... -> job/(bundle-install)
          if test("/[0-9a-f]{40,}$") then sub("/[0-9a-f]{40,}$"; "/(bundle-install)") else . end |
          sub("-[0-9a-f]{6,}$"; "") |
          sub("-[0-9]+$"; "") |
          sub("-[0-9a-z]{4,5}$"; "")
        ) | join(", "))
      ] | unique_by(.owners + .detail) | sort_by(.owners)'
}

# collect data for all versions
echo "Fetching Sippy data..." >&2
ALL_DATA="[]"
for v in "${VERSIONS[@]}"; do
  echo "  querying v${v}..." >&2

  # fetch pending namespaces for this version from GitHub
  PENDING_NS=$(fetch_pending_ns "$v" 2>/dev/null) || true
  if [[ -z "$PENDING_NS" ]]; then
    echo "    warning: could not fetch pending list from origin release-${v} branch" >&2
    PENDING_JSON="[]"
  else
    PENDING_JSON=$(echo "$PENDING_NS" | jq -R . | jq -s .)
  fi

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

# HTML mode: fetch workload details for all flaking namespaces
echo "Fetching workload details..." >&2
FLAKING_NS=$(echo "$ALL_DATA" | jq -r '[.[] | select(.no_data | not) | {version, items: (.known + .new)} | .version as $v | .items[] | {key: "\($v)|\(.test_name)", version: $v, test_name}] | unique_by(.key) | .[]| @base64')

declare -A WORKLOAD_CACHE
for entry in $FLAKING_NS; do
  decoded=$(echo "$entry" | base64 -d)
  v=$(echo "$decoded" | jq -r '.version')
  test_name=$(echo "$decoded" | jq -r '.test_name')
  ns=$(echo "$test_name" | sed 's/.*all workloads in ns\///' | sed 's/ must set the.*//')
  echo "  fetching workloads for ${v}/${ns}..." >&2
  workloads=$(fetch_workloads "$v" "$test_name")
  WORKLOAD_CACHE["${v}|${ns}"]="$workloads"
  sleep 0.1  # avoid rate limiting
done

# build a JSON object with all workload data
WORKLOAD_JSON="{}"
for key in "${!WORKLOAD_CACHE[@]}"; do
  WORKLOAD_JSON=$(echo "$WORKLOAD_JSON" | jq --arg key "$key" --argjson val "${WORKLOAD_CACHE[$key]}" '.[$key] = $val')
done

# filter out "new" entries with 0 workloads (aggregator/infra noise, not real SCC failures)
ALL_DATA=$(echo "$ALL_DATA" | jq --argjson wk "$WORKLOAD_JSON" '
  [.[] | . as $ver |
    .new |= [.[] | select(($wk["\($ver.version)|\(.ns)"] // []) | length > 0)] |
    .summary.new = (.new | length)
  ]
')

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
  .summary-card { background: #fff; border-radius: 8px; padding: 16px 20px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); min-width: 160px; cursor: pointer; transition: box-shadow 0.15s; text-decoration: none; color: inherit; }
  .summary-card:hover { box-shadow: 0 2px 8px rgba(0,0,0,0.18); }
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
  .expandable { cursor: pointer; user-select: none; }
  .expandable:hover { background: rgba(0,0,0,0.03); }
  .expand-icon { display: inline-block; width: 16px; font-size: 0.75em; color: #888; transition: transform 0.15s; }
  .details-row { display: none; }
  .details-row.open { display: table-row; }
  .details-row td { padding: 8px 10px 12px 32px; background: #f9f9f9; }
  .workload-list { list-style: none; font-size: 0.85em; }
  .workload-list li { padding: 3px 0; font-family: monospace; }
  .workload-list .scc-suggestion { color: #27ae60; font-weight: 500; }
  .workload-count { background: #eee; color: #555; font-size: 0.75em; padding: 1px 6px; border-radius: 8px; margin-left: 6px; }
</style>
</head>
<body>
HTMLHEAD

echo "<h1>SCC Pinning Status Report</h1>" >> "$HTML_OUTPUT"
echo "<p class=\"timestamp\">Generated: $(date '+%Y-%m-%d %H:%M:%S') · <a href=\"https://github.com/liouk/redhat/blob/main/issues/AUTH-482_scc-pinning/check_scc_pinning.sh\" target=\"_blank\" style=\"background:#24292e;color:#fff;padding:2px 8px;border-radius:4px;font-size:0.85em;text-decoration:none\">View source on GitHub</a></p>" >> "$HTML_OUTPUT"

# summary cards
echo '<div class="summary">' >> "$HTML_OUTPUT"
echo "$ALL_DATA" | jq -r '.[] |
  if .no_data then
    "<a class=\"summary-card\" style=\"opacity:0.5\" href=\"#version-\(.version)\"><h3>\(.version)</h3><div class=\"counts\"><span>no data</span></div></a>"
  else
    "<a class=\"summary-card\" href=\"#version-\(.version)\"><h3>\(.version)</h3><div class=\"counts\">" +
    "<span><span class=\"dot dot-flaking\"></span> \(.summary.known) flaking</span>" +
    "<span><span class=\"dot dot-new\"></span> \(.summary.new) new</span>" +
    "<span><span class=\"dot dot-fixed\"></span> \(.summary.fixed) fixed</span>" +
    "</div></a>"
  end
' >> "$HTML_OUTPUT"
echo '</div>' >> "$HTML_OUTPUT"

# render a table with expandable rows; args: jq array, row class, workloads json
render_table() {
  local items_json="$1"
  local row_class="$2"
  local workloads_json="$3"
  local version="$4"

  echo "$items_json" | jq -r --arg row_class "$row_class" --arg version "$version" --argjson wk "$workloads_json" '
    if length == 0 then
      "<p class=\"none-msg\">None</p>"
    else
      "<table><tr><th class=\"num\">Workloads</th><th>Namespace</th><th class=\"num\">Runs</th><th class=\"num\">Pass</th><th class=\"num\">Flakes</th><th class=\"num\">Fail</th><th>Sippy</th></tr>" +
      ([to_entries[] | .value as $item | .key as $idx |
        ($wk["\($version)|\($item.ns)"] // []) as $workloads |
        "<tr class=\"\($row_class) expandable\" onclick=\"document.getElementById('\''\($version)-\($row_class)-\($idx)'\'').classList.toggle('\''open'\'')\"><td class=\"num\">\($workloads | length)</td><td><span class=\"expand-icon\">&#9654;</span> \($item.ns)</td><td class=\"num\">\($item.runs)</td><td class=\"num\">\($item.pass)</td><td class=\"num\">\($item.flakes)</td><td class=\"num\">\($item.fail)</td><td><a href=\"\($item.sippy)\" target=\"_blank\" onclick=\"event.stopPropagation()\">view</a></td></tr>" +
        "<tr class=\"details-row\" id=\"\($version)-\($row_class)-\($idx)\"><td colspan=\"7\">" +
        (if ($workloads | length) > 0 then
          "<ul class=\"workload-list\">" +
          ([$workloads[] |
            "<li>\(.owners) &rarr; <span class=\"scc-suggestion\">\(.detail)</span></li>"
          ] | join("")) +
          "</ul>"
        else
          "<p class=\"none-msg\">No failure details available</p>"
        end) +
        "</td></tr>"
      ] | join("")) +
      "</table>"
    end
  '
}

# per-version sections
for v_idx in $(seq 0 $((${#VERSIONS[@]} - 1))); do
  VDATA=$(echo "$ALL_DATA" | jq ".[$v_idx]")
  VERSION=$(echo "$VDATA" | jq -r '.version')
  NO_DATA=$(echo "$VDATA" | jq -r '.no_data')

  if [[ "$NO_DATA" == "true" ]]; then
    cat >> "$HTML_OUTPUT" << NODATA
<div class="version-section" id="version-${VERSION}">
<div class="version-header" style="background:#95a5a6"><span>${VERSION}</span><span class="badges"><span style="background:#7f8c8d">no data</span></span></div>
<div class="section-group"><p class="none-msg" style="color:#e74c3c;font-style:normal">&#9888; No test data found for this version. It may not exist in Sippy yet.</p></div>
</div>
NODATA
    continue
  fi

  KNOWN_JSON=$(echo "$VDATA" | jq '.known')
  NEW_JSON=$(echo "$VDATA" | jq '.new')
  FIXED_HTML=$(echo "$VDATA" | jq -r '
    if (.fixed | length) > 0 then
      "<ul class=\"fixed-list\">" + ([.fixed[] | "<li>\(.)</li>"] | join("")) + "</ul>"
    else
      "<p class=\"none-msg\">None</p>"
    end
  ')

  BADGES=$(echo "$VDATA" | jq -r '
    (if .summary.known > 0 then "<span class=\"badge-flaking\">\(.summary.known) flaking</span>" else "" end) +
    (if .summary.new > 0 then "<span class=\"badge-new\">\(.summary.new) new</span>" else "" end) +
    (if .summary.fixed > 0 then "<span class=\"badge-fixed\">\(.summary.fixed) fixed</span>" else "" end)
  ')

  KNOWN_TABLE=$(render_table "$KNOWN_JSON" "row-flaking" "$WORKLOAD_JSON" "$VERSION")
  NEW_TABLE=$(render_table "$NEW_JSON" "row-new" "$WORKLOAD_JSON" "$VERSION")

  cat >> "$HTML_OUTPUT" << VERSIONSECTION
<div class="version-section" id="version-${VERSION}">
<div class="version-header"><span>${VERSION}</span><span class="badges">${BADGES}</span></div>
<div class="section-group"><h3>Still flaking (in pending list)</h3>${KNOWN_TABLE}</div>
<div class="section-group"><h3>New: flaking but NOT in pending list</h3>${NEW_TABLE}</div>
<div class="section-group"><h3>Fixed: can be removed from pending list</h3>${FIXED_HTML}</div>
</div>
VERSIONSECTION
done

echo "</body></html>" >> "$HTML_OUTPUT"

echo "Report written to ${HTML_OUTPUT}" >&2
xdg-open "$HTML_OUTPUT" 2>/dev/null || open "$HTML_OUTPUT" 2>/dev/null || echo "Open ${HTML_OUTPUT} in a browser" >&2
