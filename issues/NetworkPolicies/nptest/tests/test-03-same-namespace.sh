#!/bin/bash

echo "=========================================="
echo "Test 03: Allow same-namespace traffic only"
echo "=========================================="

cd "$(dirname "$0")/.."

echo ""
echo "Applying allow-same-namespace NetworkPolicies..."
oc -n nptest1 apply -f networkpolicies/allow-same-namespace.yaml
oc -n nptest2 apply -f networkpolicies/allow-same-namespace.yaml

echo ""
echo "Expecting: Same-namespace ✓, cross-namespace ✗"

echo ""
echo "Testing connectivity:"
./nptest.sh

echo ""
echo "Cleaning up NetworkPolicies..."
oc -n nptest1 delete -f networkpolicies/allow-same-namespace.yaml
oc -n nptest2 delete -f networkpolicies/allow-same-namespace.yaml

echo ""
echo "Test complete!"
echo ""
