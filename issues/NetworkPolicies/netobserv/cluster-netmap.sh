#!/usr/bin/env bash

# Check if port 3100 is already accessible
if ! curl -s http://localhost:3100/ready >/dev/null 2>&1; then
  # Port not accessible, start port-forward
  oc port-forward -n netobserv svc/loki 3100:3100 &
  PF_PID=$!
  sleep 3
else
  # Port already forwarded, reuse it
  PF_PID=""
fi

END_TIME=$(date -u +%s)000000000
START_TIME=$(date -u -d '10 minutes ago' +%s)000000000

OUTPUT_FILE="network_flows_$(date +%Y%m%d_%H%M%S).csv"

# Build namespace filter for jq
if [ $# -gt 0 ]; then
  # Store namespaces as array for jq filtering
  NAMESPACES="$*"
else
  NAMESPACES=""
fi

# Simple Loki query - filtering by namespace happens in jq
LOKI_QUERY="{app=\"netobserv-flowcollector\"}"

# Create CSV with header
echo "SourceNamespace,SourcePod,SourceIP,DestNamespace,DestPod,DestIP,Protocol,DestPort" > "$OUTPUT_FILE"

# Append data - namespace is in Loki labels, not flow body
curl -G -s "http://localhost:3100/loki/api/v1/query_range" \
  --data-urlencode "query=$LOKI_QUERY" \
  --data-urlencode "start=$START_TIME" \
  --data-urlencode "end=$END_TIME" \
  --data-urlencode 'limit=5000' | \
  jq -r --arg namespaces "$NAMESPACES" '
    ($namespaces | split(" ") | map(select(. != ""))) as $ns_filter |
    .data.result[] | .stream as $labels | .values[] |
    ($labels.SrcK8S_Namespace // "") as $srcNs |
    ($labels.DstK8S_Namespace // "") as $dstNs |
    ($labels.FlowDirection // "") as $flowDir |
    .[1] | fromjson |
    select($flowDir == "0") |
    select((.DstPort // 0) > 0 and (.DstPort // 0) < 32768) |
    select(
      if ($ns_filter | length) > 0 then
        ($ns_filter | index($srcNs)) != null or ($ns_filter | index($dstNs)) != null
      else
        true
      end
    ) |
    [$srcNs,
     .SrcK8S_Name // "",
     .SrcAddr // "",
     $dstNs,
     .DstK8S_Name // "",
     .DstAddr // "",
     (if .Proto == 6 then "TCP" elif .Proto == 17 then "UDP" else .Proto end),
     .DstPort // ""] | @csv' | \
  sort -u >> "$OUTPUT_FILE"

echo "Flows exported to: $OUTPUT_FILE"
echo "Total flows: $(wc -l < "$OUTPUT_FILE" | xargs echo $(($(cat) - 1)))"

# Only kill port-forward if we started it
if [ -n "$PF_PID" ]; then
  kill $PF_PID 2>/dev/null
fi