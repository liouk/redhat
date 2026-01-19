#!/bin/bash

echo "============================================"
echo "Test 02: Deny-all ingress on both namespaces"
echo "============================================"

cd "$(dirname "$0")/.."

echo ""
echo "Applying default-deny-all NetworkPolicies..."
oc -n nptest1 apply -f networkpolicies/default-deny-all.yaml
oc -n nptest2 apply -f networkpolicies/default-deny-all.yaml

echo ""
echo "Expecting: All connections should be ✗ (blocked)"

echo ""
echo "Testing connectivity:"
./nptest.sh

echo ""
echo "Cleaning up NetworkPolicies..."
oc -n nptest1 delete -f networkpolicies/default-deny-all.yaml
oc -n nptest2 delete -f networkpolicies/default-deny-all.yaml

echo ""
echo "Test complete!"
echo ""
