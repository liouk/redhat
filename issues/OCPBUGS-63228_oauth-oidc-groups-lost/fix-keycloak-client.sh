#!/usr/bin/env bash

# Fix Keycloak 'openshift' client configuration

KC_BASE_URL="$1"
[[ -z "$KC_BASE_URL" ]] && { echo "Error: KC_BASE_URL is required. Usage: $0 <KEYCLOAK_BASE_URL>"; exit 1; }

KC_TARGET_REALM="master"
KC_ADMIN_REALM="master"
KC_USER="admin"
KC_PASS="password"
KC_API_PREFIX=""

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo "=== Fixing Keycloak 'openshift' Client Configuration ==="

# Get admin token
echo "1. Getting admin token..."
response=$(curl -k -s -X POST "${KC_BASE_URL}/realms/${KC_ADMIN_REALM}/protocol/openid-connect/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "client_id=admin-cli" \
    -d "username=${KC_USER}" \
    -d "password=${KC_PASS}" \
    -d "grant_type=password")

token=$(echo "$response" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    if 'access_token' in data:
        print(data['access_token'])
except: pass
")

if [[ -z "$token" ]]; then
    # Try old path
    response=$(curl -k -s -X POST "${KC_BASE_URL}/auth/realms/${KC_ADMIN_REALM}/protocol/openid-connect/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "client_id=admin-cli" \
        -d "username=${KC_USER}" \
        -d "password=${KC_PASS}" \
        -d "grant_type=password")

    token=$(echo "$response" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    if 'access_token' in data:
        print(data['access_token'])
except: pass
")
    KC_API_PREFIX="/auth"
fi

if [[ -z "$token" ]]; then
    echo -e "${RED}Failed to get admin token${NC}"
    exit 1
fi
echo -e "${GREEN}   OK${NC}"
echo ""

# Get 'openshift' client details
echo "2. Getting 'openshift' client UUID..."
client_data=$(curl -k -s -X GET "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/clients?clientId=openshift" \
    -H "Authorization: Bearer ${token}")

client_uuid=$(echo "$client_data" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    if isinstance(data, list) and len(data) > 0:
        print(data[0]['id'])
except: pass
")

if [[ -z "$client_uuid" ]]; then
    echo -e "${RED}Failed to get openshift client UUID${NC}"
    exit 1
fi
echo -e "${GREEN}   UUID: $client_uuid${NC}"
echo ""

# Update client to be public
echo "3. Updating 'openshift' client to be PUBLIC..."
update_response=$(curl -k -s -w "\n%{http_code}" -X PUT \
    "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/clients/${client_uuid}" \
    -H "Authorization: Bearer ${token}" \
    -H "Content-Type: application/json" \
    -d '{
        "publicClient": true,
        "directAccessGrantsEnabled": true,
        "standardFlowEnabled": true
    }')

http_code=$(echo "$update_response" | tail -n1)

if [[ "$http_code" == "204" || "$http_code" == "200" ]]; then
    echo -e "${GREEN}   ✓ Successfully updated client configuration${NC}"
else
    echo -e "${RED}   ✗ Failed to update client (HTTP $http_code)${NC}"
    echo "$update_response" | head -n-1
    exit 1
fi
echo ""

# Verify the fix
echo "4. Testing user1 authentication with updated 'openshift' client..."
auth_response=$(curl -k -s -w "\n%{http_code}" -X POST "${KC_BASE_URL}${KC_API_PREFIX}/realms/${KC_TARGET_REALM}/protocol/openid-connect/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "client_id=openshift" \
    -d "username=user1" \
    -d "password=redhatgss" \
    -d "grant_type=password")

http_code=$(echo "$auth_response" | tail -n1)

if [[ "$http_code" == "200" ]]; then
    echo -e "${GREEN}   ✓ SUCCESS: user1 can now authenticate with 'openshift' client${NC}"
    echo ""
    echo -e "${GREEN}=== FIX COMPLETE ===${NC}"
    echo "You can now run: ./reproduce.sh ${KC_BASE_URL}"
else
    echo -e "${RED}   ✗ FAILED: user1 still cannot authenticate (HTTP $http_code)${NC}"
    body=$(echo "$auth_response" | head -n-1)
    echo "$body" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    print(f\"   Error: {data.get('error')}\")
    print(f\"   Description: {data.get('error_description')}\")
except:
    print(f\"   Raw: {sys.stdin.read()}\")
"
fi
