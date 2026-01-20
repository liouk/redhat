#!/usr/bin/env bash

oc port-forward -n netobserv svc/loki 3100:3100 &
PF_PID=$!
sleep 3

END_TIME=$(date -u +%s)000000000
START_TIME=$(date -u -d '10 minutes ago' +%s)000000000

OUTPUT_FILE="network_flows_$(date +%Y%m%d_%H%M%S).csv"

# Create CSV with header
echo "SourceNamespace,SourcePod,SourceIP,DestNamespace,DestPod,DestIP,Protocol,DestPort" > "$OUTPUT_FILE"

# Append data - namespace is in Loki labels, not flow body
curl -G -s "http://localhost:3100/loki/api/v1/query_range" \
  --data-urlencode 'query={app="netobserv-flowcollector"}' \
  --data-urlencode "start=$START_TIME" \
  --data-urlencode "end=$END_TIME" \
  --data-urlencode 'limit=5000' | \
  jq -r '.data.result[] | .stream as $labels | .values[] |
    ($labels.SrcK8S_Namespace // "") as $srcNs |
    ($labels.DstK8S_Namespace // "") as $dstNs |
    .[1] | fromjson |
    [$srcNs,
     .SrcK8S_Name // "",
     .SrcAddr // "",
     $dstNs,
     .DstK8S_Name // "",
     .DstAddr // "",
     (if .Proto == "6" then "TCP" elif .Proto == "17" then "UDP" else .Proto end),
     .DstPort // ""] | @csv' | \
  sort -u >> "$OUTPUT_FILE"

echo "Flows exported to: $OUTPUT_FILE"
echo "Total flows: $(wc -l < "$OUTPUT_FILE" | xargs echo $(($(cat) - 1)))"

kill $PF_PID 2>/dev/null