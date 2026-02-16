#!/usr/bin/env bash

# Add groups mapper to Keycloak 'openshift' client

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
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}=== Adding Groups Mapper to 'openshift' Client ===${NC}\n"

# Get admin token
echo -e "${YELLOW}1. Getting admin token...${NC}"
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
echo -e "${GREEN}   ✓ OK${NC}\n"

# Get 'openshift' client UUID
echo -e "${YELLOW}2. Getting 'openshift' client UUID...${NC}"
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
echo -e "${GREEN}   ✓ UUID: $client_uuid${NC}\n"

# Check if groups mapper already exists
echo -e "${YELLOW}3. Checking for existing groups mapper...${NC}"
existing_mappers=$(curl -k -s -X GET \
    "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/clients/${client_uuid}/protocol-mappers/models" \
    -H "Authorization: Bearer ${token}")

groups_mapper_exists=$(echo "$existing_mappers" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    for mapper in data:
        if mapper.get('name') == 'groups' or mapper.get('protocolMapper') == 'oidc-group-membership-mapper':
            print('YES')
            print(f\"Mapper: {mapper.get('name')}\", file=sys.stderr)
            exit(0)
    print('NO')
except:
    print('ERROR')
" 2>&1)

mapper_name=$(echo "$groups_mapper_exists" | grep "Mapper:" | cut -d' ' -f2)
groups_mapper_exists=$(echo "$groups_mapper_exists" | head -1)

if [[ "$groups_mapper_exists" == "YES" ]]; then
    echo -e "${YELLOW}   ℹ Groups mapper already exists: $mapper_name${NC}"
    echo -e "${YELLOW}   Checking configuration...${NC}\n"
else
    echo -e "${YELLOW}   No groups mapper found, creating one...${NC}\n"
fi

# Create/Update groups mapper
echo -e "${YELLOW}4. Creating groups protocol mapper...${NC}"

mapper_json='{
    "name": "groups",
    "protocol": "openid-connect",
    "protocolMapper": "oidc-group-membership-mapper",
    "consentRequired": false,
    "config": {
        "full.path": "false",
        "id.token.claim": "true",
        "access.token.claim": "true",
        "claim.name": "groups",
        "userinfo.token.claim": "true"
    }
}'

create_response=$(curl -k -s -w "\n%{http_code}" -X POST \
    "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/clients/${client_uuid}/protocol-mappers/models" \
    -H "Authorization: Bearer ${token}" \
    -H "Content-Type: application/json" \
    -d "$mapper_json")

http_code=$(echo "$create_response" | tail -n1)
body=$(echo "$create_response" | head -n-1)

if [[ "$http_code" == "201" || "$http_code" == "204" ]]; then
    echo -e "${GREEN}   ✓ Groups mapper created successfully${NC}\n"
elif [[ "$http_code" == "409" ]]; then
    echo -e "${YELLOW}   ℹ Groups mapper already exists (HTTP 409)${NC}\n"
else
    echo -e "${YELLOW}   Note: Received HTTP $http_code (may already exist)${NC}"
    if [[ -n "$body" ]]; then
        echo "$body" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    if 'errorMessage' in data:
        print(f\"   Message: {data['errorMessage']}\")
except: pass
" || echo "   Response: $body"
    fi
    echo ""
fi

# Verify the mapper is configured
echo -e "${YELLOW}5. Verifying groups mapper configuration...${NC}"
updated_mappers=$(curl -k -s -X GET \
    "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/clients/${client_uuid}/protocol-mappers/models" \
    -H "Authorization: Bearer ${token}")

echo "$updated_mappers" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    for mapper in data:
        if mapper.get('protocolMapper') == 'oidc-group-membership-mapper':
            print(f\"   ✓ Found groups mapper: {mapper.get('name')}\")
            config = mapper.get('config', {})
            print(f\"     - Claim name: {config.get('claim.name')}\")
            print(f\"     - Access token: {config.get('access.token.claim')}\")
            print(f\"     - ID token: {config.get('id.token.claim')}\")
            print(f\"     - Userinfo: {config.get('userinfo.token.claim')}\")
            print(f\"     - Full path: {config.get('full.path')}\")
            exit(0)
    print('   ✗ Groups mapper not found!')
    exit(1)
except Exception as e:
    print(f'   Error: {e}')
    exit(1)
"

if [ $? -eq 0 ]; then
    echo ""
    echo -e "${GREEN}=== SUCCESS ===${NC}"
    echo -e "${GREEN}Groups mapper is now configured!${NC}\n"
    echo "Next steps:"
    echo "  1. Test: ./debug-oidc-sync.sh ${KC_BASE_URL} user2 group12"
    echo "  2. Run: ./reproduce.sh ${KC_BASE_URL}"
else
    echo -e "${RED}Failed to verify groups mapper${NC}"
    exit 1
fi
