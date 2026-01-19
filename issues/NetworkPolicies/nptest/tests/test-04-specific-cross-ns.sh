#!/bin/bash

echo "================================================"
echo "Test 04: Allow only pod1 cross-namespace traffic"
echo "================================================"

cd "$(dirname "$0")/.."

echo ""
echo "Applying default-deny-all NetworkPolicies..."
oc -n nptest1 apply -f networkpolicies/default-deny-all.yaml
oc -n nptest2 apply -f networkpolicies/default-deny-all.yaml

echo ""
echo "Applying specific allow NetworkPolicies..."
oc apply -f networkpolicies/allow-specific-cross-ns.yaml

echo ""
echo "Expecting:"
echo "  nptest1-pod1 -> nptest2-pod1: ✓"
echo "  nptest2-pod1 -> nptest1-pod1: ✓"
echo "  All other connections (including any involving pod2): ✗"

echo ""
echo "Testing connectivity:"
./nptest.sh

echo ""
echo "Cleaning up NetworkPolicies..."
oc -n nptest1 delete -f networkpolicies/default-deny-all.yaml
oc -n nptest2 delete -f networkpolicies/default-deny-all.yaml
oc delete -f networkpolicies/allow-specific-cross-ns.yaml

echo ""
echo "Test complete!"
echo ""
