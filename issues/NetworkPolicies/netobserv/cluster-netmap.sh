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

# Build Loki query with namespace and FlowDirection filters
if [ $# -gt 0 ]; then
  # Join namespace arguments with | for regex matching
  NAMESPACE_REGEX=$(IFS='|'; echo "$*")
  # Query for flows where EITHER source OR destination namespace matches
  # FlowDirection="0" filters for egress/initiator flows at Loki level
  LOKI_QUERY="{app=\"netobserv-flowcollector\", FlowDirection=\"0\", SrcK8S_Namespace=~\"$NAMESPACE_REGEX\"} or {app=\"netobserv-flowcollector\", FlowDirection=\"0\", DstK8S_Namespace=~\"$NAMESPACE_REGEX\"}"
else
  # No namespace filter - just get all egress flows
  LOKI_QUERY="{app=\"netobserv-flowcollector\", FlowDirection=\"0\"}"
fi

# Create CSV with header
echo "SourceNamespace,SourcePod,SourceIP,DestNamespace,DestPod,DestIP,Protocol,DestPort" > "$OUTPUT_FILE"

# Function to process flows from Loki
process_flows() {
  jq -r '
    .data.result[] | .stream as $labels | .values[] |
    ($labels.SrcK8S_Namespace // "") as $srcNs |
    ($labels.DstK8S_Namespace // "") as $dstNs |
    .[1] | fromjson |
    # Only filter by destination port - everything else filtered by Loki
    select((.DstPort // 0) > 0 and (.DstPort // 0) < 32768) |
    [$srcNs,
     .SrcK8S_Name // "",
     .SrcAddr // "",
     $dstNs,
     .DstK8S_Name // "",
     .DstAddr // "",
     (if .Proto == 6 then "TCP" elif .Proto == 17 then "UDP" else .Proto end),
     .DstPort // ""] | @csv'
}

# Append data - query Loki with pre-filters
if [ $# -gt 0 ]; then
  # Query for source namespace matches
  curl -G -s "http://localhost:3100/loki/api/v1/query_range" \
    --data-urlencode "query={app=\"netobserv-flowcollector\", FlowDirection=\"0\", SrcK8S_Namespace=~\"$NAMESPACE_REGEX\"}" \
    --data-urlencode "start=$START_TIME" \
    --data-urlencode "end=$END_TIME" \
    --data-urlencode 'limit=5000' 2>/dev/null | process_flows >> "$OUTPUT_FILE.tmp"

  # Query for destination namespace matches
  curl -G -s "http://localhost:3100/loki/api/v1/query_range" \
    --data-urlencode "query={app=\"netobserv-flowcollector\", FlowDirection=\"0\", DstK8S_Namespace=~\"$NAMESPACE_REGEX\"}" \
    --data-urlencode "start=$START_TIME" \
    --data-urlencode "end=$END_TIME" \
    --data-urlencode 'limit=5000' 2>/dev/null | process_flows >> "$OUTPUT_FILE.tmp"
else
  # No namespace filter
  curl -G -s "http://localhost:3100/loki/api/v1/query_range" \
    --data-urlencode "query=$LOKI_QUERY" \
    --data-urlencode "start=$START_TIME" \
    --data-urlencode "end=$END_TIME" \
    --data-urlencode 'limit=5000' 2>/dev/null | process_flows >> "$OUTPUT_FILE.tmp"
fi

# Sort and deduplicate
sort -u "$OUTPUT_FILE.tmp" >> "$OUTPUT_FILE"
rm -f "$OUTPUT_FILE.tmp"

echo "Flows exported to: $OUTPUT_FILE"
echo "Total flows: $(wc -l < "$OUTPUT_FILE" | xargs echo $(($(cat) - 1)))"

# Only kill port-forward if we started it
if [ -n "$PF_PID" ]; then
  kill $PF_PID 2>/dev/null
fi