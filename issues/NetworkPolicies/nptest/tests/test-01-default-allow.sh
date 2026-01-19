#!/bin/bash

echo "==========================================="
echo "Test 01: Default allow (no NetworkPolicies)"
echo "==========================================="

echo ""
echo "Expecting: All connections should be ✓ (allowed)"
echo ""

echo ""
echo "Testing connectivity:"
cd "$(dirname "$0")/.." && ./nptest.sh

echo ""
echo "Test complete!"
echo ""