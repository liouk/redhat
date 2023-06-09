#! /usr/bin/env bash

set -e
set -o pipefail
[ -n "$TRACE" ] && set -x

oc login -u "system:admin" >/dev/null
echo "logged in as $(oc whoami)"

user="${1:-}"
userUID=$(oc get user ${user:=ilias} -oyaml | grep uid | cut -d" " -f4)

# create role for test user
oc delete ns test || true
oc delete rolebinding myrb || true
oc delete role myrole || true
oc delete rolebinding myrb-writer || true
oc delete role myrole-writer || true
oc delete pod nginx || true
cat <<EOF | oc apply -f -
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: myrole
  namespace: default
rules:
- apiGroups:
  - ""
  resources:
  - pods
  verbs:
  - list
  - get
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: myrb
  namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: myrole
subjects:
- kind: User
  name: ilias
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: myrole-writer
  namespace: default
rules:
- apiGroups:
  - ""
  resources:
  - pods
  verbs:
  - create
  - update
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: myrb-writer
  namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: myrole-writer
subjects:
- kind: User
  name: ilias
---
apiVersion: v1
kind: Namespace
metadata:
  name: test
EOF

# (re)create test tokens
token_f="sha256~3jWRPKDnhizSVdZEmAMbQaU6FKGr8O2XuJXG2utA4k8"
hash_f="sha256~44yteqXEA9zXAFifKmKyyiUwpOk5ZhUuUyd_s-WkNQY"
oc delete oauthaccesstoken "$hash_f" || true
oc create -f - <<EOF || true
apiVersion: oauth.openshift.io/v1
kind: OAuthAccessToken
metadata:
  name: $hash_f
expiresIn: 86400
userName: ilias
userUID: $userUID
scopes:
- user:full
clientName: openshift-browser-client
redirectURI: https://oauth-openshift.apps.ci-ln-yl9pl1b-76ef8.aws-2.ci.openshift.org/oauth/token/display
EOF
scopes_f=$(oc get oauthaccesstoken $hash_f -o=custom-columns=SCOPES:.scopes | tail -n +2)

token_ca="sha256~74nvCq63mA7c7fwkbqunvtoHv3i_4f_L3ToIX-vqEAg"
hash_ca="sha256~nERCZRl1SLWsQJ1LO6uZqwtThWiMMWjhyOsCLEA10E8"
oc delete oauthaccesstoken "$hash_ca" || true
oc create -f - <<EOF || true
apiVersion: oauth.openshift.io/v1
kind: OAuthAccessToken
metadata:
  name: $hash_ca
expiresIn: 86400
userName: ilias
userUID: $userUID
scopes:
- user:check-access
clientName: openshift-browser-client
redirectURI: https://oauth-openshift.apps.ci-ln-yl9pl1b-76ef8.aws-2.ci.openshift.org/oauth/token/display
EOF
scopes_ca=$(oc get oauthaccesstoken $hash_ca -o=custom-columns=SCOPES:.scopes | tail -n +2)

token_cai="sha256~_4Q8XXjg4ehB9E27H0FHC83mxKJBKJDIp5YgkiJn-kI"
hash_cai="sha256~oVxMVt0HFc05cv9pbzaL94Xl0IWDH9tdb51EaUESW00"
oc delete oauthaccesstoken "$hash_cai" || true
oc create -f - <<EOF || true
apiVersion: oauth.openshift.io/v1
kind: OAuthAccessToken
metadata:
  name: $hash_cai
expiresIn: 86400
userName: ilias
userUID: $userUID
scopes:
- user:check-access
- user:info
clientName: openshift-browser-client
redirectURI: https://oauth-openshift.apps.ci-ln-yl9pl1b-76ef8.aws-2.ci.openshift.org/oauth/token/display
EOF
scopes_cai=$(oc get oauthaccesstoken $hash_cai -o=custom-columns=SCOPES:.scopes | tail -n +2)

token_inf="sha256~cQCrXRpBdghJ9EjcHBJs3fAG8hhusl20B1YwZ_WwQF8"
hash_inf="sha256~jwahvfqk7YeWkPkqTeOHJbKE4uDUqsoP1XhRHvk5--g"
oc delete oauthaccesstoken "$hash_inf" || true
oc create -f - <<EOF || true
apiVersion: oauth.openshift.io/v1
kind: OAuthAccessToken
metadata:
  name: $hash_inf
expiresIn: 86400
userName: ilias
userUID: $userUID
scopes:
- user:info
clientName: openshift-browser-client
redirectURI: https://oauth-openshift.apps.ci-ln-yl9pl1b-76ef8.aws-2.ci.openshift.org/oauth/token/display
EOF
scopes_inf=$(oc get oauthaccesstoken $hash_inf -o=custom-columns=SCOPES:.scopes | tail -n +2)

token_role="sha256~RmcujdrsbNvRDl_-sEzp1sx6HbP-2ZlmFnvNY6rEN14"
hash_role="sha256~FlazKBhtre9nB0rnf_Tq1PB4b_7nI_a48H_K605x_f4"
oc delete oauthaccesstoken "$hash_role" || true
oc create -f - <<EOF || true
apiVersion: oauth.openshift.io/v1
kind: OAuthAccessToken
metadata:
  name: $hash_role
expiresIn: 86400
userName: ilias
userUID: $userUID
scopes:
- user:check-access
- role:myrole:default
clientName: openshift-browser-client
redirectURI: https://oauth-openshift.apps.ci-ln-yl9pl1b-76ef8.aws-2.ci.openshift.org/oauth/token/display
EOF
scopes_role=$(oc get oauthaccesstoken $hash_role -o=custom-columns=SCOPES:.scopes | tail -n +2)

echo
oc get oauthaccesstokens -o=custom-columns='USERNAME:.userName,NAME:.metadata.name,SCOPES:.scopes,CREATED:.metadata.creationTimestamp'

echo
echo "######[ TESTS ]######"

echo -e "\n*** Checking self-SAR via user login"
oc login -u ilias -p ilias >/dev/null
echo "logged in as $(oc whoami)"
for ns in default test; do
  echo -e "\nNAMESPACE: $ns"
  echo -n "  can-i get pods? "; oc auth can-i get pods -n $ns || true
  echo -n "  can-i list pods? "; oc auth can-i list pods -n $ns || true
  echo -n "  can-i create pods? "; oc auth can-i create pods -n $ns || true
  echo -n "  can-i update pods? "; oc auth can-i update pods -n $ns || true
  echo -n "  can-i watch pods? "; oc auth can-i watch pods -n $ns || true
  echo -n "  can-i create self-SARs? "; oc auth can-i create selfsubjectaccessreviews || true
done

# test SelfSARs with can-i; run can-i with -v10 to inspect the actual SelfSubjectAccessReview request
tokens=(        "$token_f"      "$token_ca"      "$token_cai"     "$token_inf"     "$token_role"  "$token_role")
hashes=(        "$hash_f"       "$hash_ca"       "$hash_cai"      "$hash_inf"      "$hash_role"   "$hash_role")
scopes=(        "$scopes_f"     "$scopes_ca"     "$scopes_cai"    "$scopes_inf"    "$scopes_role" "$scopes_role")
namespaces=(    "default"       "default"        "default"        "default"        "default"      "test")
exp_get=(       "yes"           "yes"            "yes"            "scopes prevent" "yes"          "no")
exp_do_get=(    "ok"            "scopes prevent" "scopes prevent" "scopes prevent" "yes"          "no")
exp_create=(    "yes"           "yes"            "yes"            "scopes prevent" "no"           "no")
exp_do_create=( "ok"            "scopes prevent" "scopes prevent" "scopes prevent" "no"           "no")

echo -e "\n*** Checking self-SAR via token login"
for i in ${!tokens[@]}; do
# for i in 4 5; do
  echo
  echo "hash:      ${hashes[$i]}"
  echo "token:     ${tokens[$i]}"
  echo "scopes:    ${scopes[$i]}"
  echo "namespace: ${namespaces[$i]}"

  oc login --token="${tokens[$i]}" >/dev/null

  printf "===> can-i get pods? (expected: ${exp_get[$i]})\n"
  oc auth can-i get pods -n ${namespaces[$i]} || true

  printf "===> can-i create pods? (expected: ${exp_create[$i]})\n"
  oc auth can-i create pods -n ${namespaces[$i]} || true

  printf "===> get pods? (expected: ${exp_do_get[$i]})\n"
  oc -n ${namespaces[$i]} get pods || true

  printf "===> create pods? (expected: ${exp_do_create[$i]})\n"
  oc -n ${namespaces[$i]} run nginx --image=nginx --port=80 --restart=Never || true

  printf "===> create self-SARs?\n"
  oc auth can-i create selfsubjectaccessreviews || true
done
