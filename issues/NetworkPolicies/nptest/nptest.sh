#!/bin/bash

# Define pods and namespaces
declare -A pods=(
    ["nptest1-pod1"]="nptest1"
    ["nptest1-pod2"]="nptest1"
    ["nptest2-pod1"]="nptest2"
    ["nptest2-pod2"]="nptest2"
)

# Get pod IPs
declare -A pod_ips
for pod in "${!pods[@]}"; do
    ns="${pods[$pod]}"
    ip=$(oc get pod "$pod" -n "$ns" -o jsonpath='{.status.podIP}' 2>/dev/null)
    if [ -z "$ip" ]; then
        echo "ERROR: Could not get IP for $pod in $ns"
        exit 1
    fi
    pod_ips["$pod"]="$ip"
done

echo "Testing pod-to-pod connectivity (port 8080)..."
echo "=============================================="

# Test connectivity from each pod to all others
for src_pod in "${!pods[@]}"; do
    src_ns="${pods[$src_pod]}"

    for dst_pod in "${!pods[@]}"; do
        # Skip self-connection
        [ "$src_pod" = "$dst_pod" ] && continue

        dst_ip="${pod_ips[$dst_pod]}"

        # Test with curl with 2 second timeout
        if oc exec "$src_pod" -n "$src_ns" -- curl -s -m 2 "http://$dst_ip:8080" &>/dev/null; then
            result="✓"
        else
            result="✗"
        fi

        printf "%-18s -> %-18s : %s\n" "$src_pod" "$dst_pod" "$result"
    done
done
