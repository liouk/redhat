#!/usr/bin/env bash

# script provided by @cchen, written by AI

# ==========================================
# 1. CONFIGURATION
# ==========================================
KC_BASE_URL="$1"
SUFFIX="${2:-}"  # Optional suffix (default: empty)

[[ -z "$KC_BASE_URL" ]] && {
    echo "Error: KC_BASE_URL is required."
    echo "Usage: $0 <KEYCLOAK_BASE_URL> [SUFFIX]"
    echo "Example: $0 https://keycloak.example.com xyz"
    echo "  (creates user1-xyz, user2-xyz, group1-xyz, group2-xyz, etc.)"
    exit 1
}

KC_TARGET_REALM="master"
KC_ADMIN_REALM="master"
KC_USER="admin"
KC_PASS="password"

# Test Parameters
NUM_USERS=20
NUM_GROUPS=20
COMMON_PASSWORD="redhatgss"
OC_API_URL="$(oc whoami --show-server)"

# Suffix for user/group names (added with dash if provided)
if [[ -n "$SUFFIX" ]]; then
    NAME_SUFFIX="-${SUFFIX}"
    echo "Using suffix: $SUFFIX (user/group names will be like user1-${SUFFIX}, group1-${SUFFIX})"
else
    NAME_SUFFIX=""
fi

# Kubeconfig Isolation
ADMIN_KUBECONFIG=${KUBECONFIG:-"$HOME/.kube/config"}
USER_KUBECONFIG="/tmp/chaos_test_user_kubeconfig"

# Arrays to Cache UUIDs
declare -a USER_IDS
declare -a GROUP_IDS

# Keycloak API path prefix (will be detected)
KC_API_PREFIX=""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ==========================================
# 2. HELPER FUNCTIONS
# ==========================================

_get_kc_token() {
    # Auto-detect Keycloak API path on first call
    if [[ -z "$KC_API_PREFIX" ]]; then
        # Try new Keycloak path first (v17+)
        local response=$(curl -k -s -X POST "${KC_BASE_URL}/realms/${KC_ADMIN_REALM}/protocol/openid-connect/token" \
            -H "Content-Type: application/x-www-form-urlencoded" \
            -d "client_id=admin-cli" \
            -d "username=${KC_USER}" \
            --data-urlencode "password=${KC_PASS}" \
            -d "grant_type=password")

        local token=$(echo "$response" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    if 'access_token' in data:
        print(data['access_token'])
except:
    pass
")

        if [[ -n "$token" ]]; then
            KC_API_PREFIX=""
            echo "$token"
            return 0
        fi

        # Try old path (pre-v17)
        response=$(curl -k -s -X POST "${KC_BASE_URL}/auth/realms/${KC_ADMIN_REALM}/protocol/openid-connect/token" \
            -H "Content-Type: application/x-www-form-urlencoded" \
            -d "client_id=admin-cli" \
            -d "username=${KC_USER}" \
            --data-urlencode "password=${KC_PASS}" \
            -d "grant_type=password")

        token=$(echo "$response" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    if 'access_token' in data:
        print(data['access_token'])
    elif 'error' in data:
        print('Keycloak Error:', data.get('error'), '-', data.get('error_description', ''), file=sys.stderr)
except Exception as e:
    print('Failed to parse token response:', e, file=sys.stderr)
")

        if [[ -n "$token" ]]; then
            KC_API_PREFIX="/auth"
            echo "$token"
            return 0
        fi

        echo -e "${RED}Failed to obtain Keycloak token. Check credentials and URL.${NC}" >&2
        echo "Response: $response" >&2
        exit 1
    else
        # Use cached prefix
        local response=$(curl -k -s -X POST "${KC_BASE_URL}${KC_API_PREFIX}/realms/${KC_ADMIN_REALM}/protocol/openid-connect/token" \
            -H "Content-Type: application/x-www-form-urlencoded" \
            -d "client_id=admin-cli" \
            -d "username=${KC_USER}" \
            --data-urlencode "password=${KC_PASS}" \
            -d "grant_type=password")

        local token=$(echo "$response" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    if 'access_token' in data:
        print(data['access_token'])
except:
    pass
")
        echo "$token"
    fi
}

# Check if a User ID is currently a member of Group ID in Keycloak
_is_kc_member() {
    local token="$1"
    local uid="$2"
    local gid="$3"

    # Fetch user's groups and check if gid exists in the list
    curl -k -s -X GET "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/users/${uid}/groups" \
        -H "Authorization: Bearer ${token}" | \
        python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    if isinstance(data, list):
        print('YES' if any(g.get('id') == '$gid' for g in data) else 'NO')
    else:
        print('ERROR', file=sys.stderr)
        print('Expected list, got:', type(data).__name__, file=sys.stderr)
        print('NO')
except Exception as e:
    print('ERROR', file=sys.stderr)
    print('Error parsing JSON:', e, file=sys.stderr)
    print('NO')
"
}

# ==========================================
# 3. INITIALIZATION
# ==========================================

init_and_cache() {
    echo -e "${BLUE}=== Phase 1: Initialization & Caching ===${NC}"
    local token=$(_get_kc_token)

    echo -e "${YELLOW}>> Ensuring $NUM_GROUPS Groups exist...${NC}"
    for ((i=1; i<=NUM_GROUPS; i++)); do
        local gname="group${i}${NAME_SUFFIX}"
        # Create (silently fail if exists)
        curl -k -s -o /dev/null -X POST "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/groups" \
            -H "Authorization: Bearer ${token}" -H "Content-Type: application/json" -d "{\"name\": \"$gname\"}"

        # Cache ID
        local gid=$(curl -k -s -X GET "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/groups?search=${gname}" \
            -H "Authorization: Bearer ${token}" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    if isinstance(data, list):
        print(next((g['id'] for g in data if g.get('name') == '$gname'), ''))
    else:
        print('', file=sys.stderr)
        print('Error: Expected list, got:', type(data).__name__, file=sys.stderr)
except Exception as e:
    print('', file=sys.stderr)
    print('Error parsing JSON:', e, file=sys.stderr)
")
        if [[ -z "$gid" ]]; then
            echo -e "\n${RED}Error: Failed to get ID for $gname${NC}"
            echo "Debug: Check Keycloak API response manually:"
            echo "  curl -k -s -X GET \"${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/groups?search=${gname}\" -H \"Authorization: Bearer ${token}\""
            exit 1
        fi
        GROUP_IDS[$i]=$gid
        echo -ne "   Cached $gname\r"
    done
    echo ""

    echo -e "${YELLOW}>> Ensuring $NUM_USERS Users exist...${NC}"
    for ((i=1; i<=NUM_USERS; i++)); do
        local uname="user${i}${NAME_SUFFIX}"
        # Create
        curl -k -s -o /dev/null -X POST "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/users" \
            -H "Authorization: Bearer ${token}" -H "Content-Type: application/json" -d "{\"username\": \"$uname\", \"enabled\": true}"

        # Cache ID
        local uid=$(curl -k -s -X GET "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/users?username=${uname}" \
            -H "Authorization: Bearer ${token}" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    if isinstance(data, list):
        print(next((u['id'] for u in data if u.get('username') == '$uname'), ''))
    else:
        print('', file=sys.stderr)
        print('Error: Expected list, got:', type(data).__name__, file=sys.stderr)
except Exception as e:
    print('', file=sys.stderr)
    print('Error parsing JSON:', e, file=sys.stderr)
")
        if [[ -z "$uid" ]]; then
            echo -e "\n${RED}Error: Failed to get ID for $uname${NC}"
            echo "Debug: Check Keycloak API response manually:"
            echo "  curl -k -s -X GET \"${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/users?username=${uname}\" -H \"Authorization: Bearer ${token}\""
            exit 1
        fi
        USER_IDS[$i]=$uid

        # Set Password
        local pwd_response=$(curl -k -s -w "\n%{http_code}" -X PUT "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/users/${uid}/reset-password" \
            -H "Authorization: Bearer ${token}" -H "Content-Type: application/json" -d "{\"type\": \"password\", \"value\": \"$COMMON_PASSWORD\", \"temporary\": false}")
        local pwd_http_code=$(echo "$pwd_response" | tail -n1)
        if [[ "$pwd_http_code" != "204" && "$pwd_http_code" != "200" ]]; then
            echo -e "\n${RED}Warning: Password set returned HTTP $pwd_http_code for $uname${NC}"
            echo "Response: $(echo "$pwd_response" | head -n-1)"
        fi
        echo -ne "   Cached $uname (pwd: $pwd_http_code)\r"
    done
    echo -e "\n${GREEN}Initialization Complete.${NC}"

    # --- Verify user can authenticate to Keycloak with 'openshift' client ---
    local test_user="user1${NAME_SUFFIX}"
    echo -e "${YELLOW}>> Testing Keycloak authentication for $test_user with 'openshift' client...${NC}"
    local test_response=$(curl -k -s -w "\n%{http_code}" -X POST "${KC_BASE_URL}${KC_API_PREFIX}/realms/${KC_TARGET_REALM}/protocol/openid-connect/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "client_id=openshift" \
        -d "username=$test_user" \
        -d "password=${COMMON_PASSWORD}" \
        -d "grant_type=password")
    local test_http_code=$(echo "$test_response" | tail -n1)
    local test_body=$(echo "$test_response" | head -n-1)

    if [[ "$test_http_code" == "200" ]]; then
        echo -e "${GREEN}   [OK] $test_user can authenticate to Keycloak with 'openshift' client${NC}"
    else
        echo -e "${RED}   [FAIL] $test_user cannot authenticate to Keycloak with 'openshift' client (HTTP $test_http_code)${NC}"
        echo "$test_body" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    print('   Error: ' + data.get('error', 'unknown'))
    print('   Description: ' + data.get('error_description', 'none'))
except:
    pass
" || echo "   Response: $test_body"
        echo -e "${YELLOW}   HINT: Check if 'openshift' client has 'Direct Access Grants' enabled${NC}"
        echo -e "${YELLOW}   Run: ./check-keycloak-client.sh ${KC_BASE_URL}${NC}"
    fi
}

# ==========================================
# 4. VERIFY FUNCTION (OpenShift Side)
# ==========================================

perform_login_and_verify() {
    local user=$1
    local group=$2
    local expect=$3 # "present" or "absent"

    # --- A. User Login (Trigger Sync) ---
    export KUBECONFIG=$USER_KUBECONFIG
    rm -f $USER_KUBECONFIG

    echo -e "${YELLOW}   [DEBUG] Attempting login: user=$user, API=$OC_API_URL${NC}"
    local login_output=$(oc login "$OC_API_URL" -u "$user" -p "$COMMON_PASSWORD" --insecure-skip-tls-verify -v=6 2>&1)
    local login_status=$?

    if [ $login_status -ne 0 ]; then
        echo -e "${RED}   [Error] Login failed for $user (exit code: $login_status)${NC}"
        echo -e "${RED}   [Error Output] $login_output${NC}"
        return 1
    fi

    # Check if OAuth flow was used
    if echo "$login_output" | grep -q "oauth"; then
        echo -e "${GREEN}   [DEBUG] Login successful for $user (OAuth flow detected)${NC}"
    else
        echo -e "${YELLOW}   [DEBUG] Login successful for $user (but no OAuth in logs - checking...)${NC}"
        echo "$login_output" | grep -i "auth\|token\|oidc" | head -5
    fi

    # Save kubeconfig for inspection
    cp "$USER_KUBECONFIG" "/tmp/kubeconfig_${user}_last.yaml" 2>/dev/null

    # --- B. Verification Loop ---
    export KUBECONFIG=$ADMIN_KUBECONFIG

    for (( i=1; i<=10; i++ )); do
        # Get Group JSON safely
        local get_output=$(oc get group "$group" -o json 2>&1)

        # Check if group exists/user is in list via Python
        local status=$(echo "$get_output" | python3 -c "
import sys, json
try:
    data = json.loads(sys.stdin.read())
    users = data.get('users') or []
    print('FOUND' if '$user' in users else 'MISSING')
except:
    print('ERROR')
")

        # Success Conditions
        if [[ "$expect" == "present" && "$status" == "FOUND" ]]; then
            echo -e "   [OK] $user detected in $group (Sync: ${i}s)."
            return 0
        fi
        if [[ "$expect" == "absent" && ( "$status" == "MISSING" || "$get_output" == *"NotFound"* ) ]]; then
            echo -e "   [OK] $user removed/absent from $group (Sync: ${i}s)."
            return 0
        fi

        sleep 1
    done

    # Fail
    echo -e "${RED}   [FAIL] Timeout waiting for $user to be $expect in $group.${NC}"
    echo -e "${YELLOW}   Current group status:${NC}"
    oc get group "$group" -o json 2>&1 | python3 -c "
import sys, json
try:
    data = json.loads(sys.stdin.read())
    users = data.get('users', [])
    print(f'     Members: {users if users else \"(empty)\"}')
except:
    output = sys.stdin.read()
    print(f'     Error: {output}')
"
    echo -e "${YELLOW}   Running detailed debug...${NC}"
    ./debug-oidc-sync.sh "$KC_BASE_URL" "$user" "$group"
    exit 1
}

# ==========================================
# 5. CHAOS LOOP
# ==========================================

run_chaos() {
    echo -e "${BLUE}=== Phase 2: Running Chaos Simulation (Random Walk) ===${NC}"
    local count=1

    while true; do
        local token=$(_get_kc_token)

        # 1. Pick ANY Random User and ANY Random Group
        local u_idx=$((1 + RANDOM % NUM_USERS))
        local g_idx=$((1 + RANDOM % NUM_GROUPS))

        local uname="user${u_idx}${NAME_SUFFIX}"
        local uid=${USER_IDS[$u_idx]}
        local gname="group${g_idx}${NAME_SUFFIX}"
        local gid=${GROUP_IDS[$g_idx]}

        echo -e "${YELLOW}Iteration $count: Selected $uname & $gname${NC}"

        # 2. Check Keycloak State: Is the user ALREADY in the group?
        local is_member=$(_is_kc_member "$token" "$uid" "$gid")

        if [[ "$is_member" == "YES" ]]; then
            # --- CASE: ALREADY MEMBER -> REMOVE ---
            echo -ne "   [State: Member] -> Action: REMOVING... "
            curl -k -s -o /dev/null -X DELETE \
                "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/users/${uid}/groups/${gid}" \
                -H "Authorization: Bearer ${token}"
            echo "Done."

            perform_login_and_verify "$uname" "$gname" "absent"

        else
            # --- CASE: NOT MEMBER -> ADD ---
            echo -ne "   [State: Stranger] -> Action: ADDING... "
            curl -k -s -o /dev/null -X PUT \
                "${KC_BASE_URL}${KC_API_PREFIX}/admin/realms/${KC_TARGET_REALM}/users/${uid}/groups/${gid}" \
                -H "Authorization: Bearer ${token}"
            echo "Done."

            perform_login_and_verify "$uname" "$gname" "present"
        fi

        echo -e "---------------------------------------------------"
        ((count++))
    done
}

# ==========================================
# EXECUTION
# ==========================================
if [[ -z "$ADMIN_KUBECONFIG" ]]; then
    echo "Error: KUBECONFIG not set."
    exit 1
fi

init_and_cache
run_chaos
