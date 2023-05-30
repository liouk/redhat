#! /usr/bin/env bash

set -e

oc login -u "system:admin"

user="${1:-}"
userUID=$(oc get user ${user:=ilias} -oyaml | grep uid | cut -d" " -f4)
printf "\nuser UID: $userUID\n"

oc adm policy add-role-to-user cluster-reader ${user:=ilias}

token_f="sha256~3jWRPKDnhizSVdZEmAMbQaU6FKGr8O2XuJXG2utA4k8"
hash_f="sha256~44yteqXEA9zXAFifKmKyyiUwpOk5ZhUuUyd_s-WkNQY"
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

echo
oc get oauthaccesstokens -o=custom-columns='USERNAME:.userName,NAME:.metadata.name,SCOPES:.scopes,CREATED:.metadata.creationTimestamp'

echo
echo "######[ TESTS ]######"

# test SelfSARs with can-i; run can-i with -v10 to inspect the actual SelfSubjectAccessReview request
tokens=(        "$token_f"      "$token_ca"      "$token_cai"     "$token_inf")
hashes=(        "$hash_f"       "$hash_ca"       "$hash_cai"      "$hash_inf")
scopes=(        "$scopes_f"     "$scopes_ca"     "$scopes_cai"    "$scopes_inf")
exp_get=(       "yes"           "yes"            "yes"            "scope prevents")
exp_do_get=(    "yes"           "scope prevents" "scope prevents" "scope prevents")
exp_create=(    "no"            "no"             "no"             "scope prevents")
exp_do_create=( "wip"           "wip"            "wip"            "wip")
exp_do_create=( "role prevents" "scope prevents" "scope prevents" "scope prevents")

for i in ${!tokens[@]}; do
  echo
  echo "hash:   ${hashes[$i]}"
  echo "token:  ${tokens[$i]}"
  echo "scopes: ${scopes[$i]}"

  oc login --token="${tokens[$i]}" >/dev/null

  printf "===> can-i get pods? (expected: ${exp_get[$i]})\n"
  oc auth can-i get pods || true

  printf "===> can-i create pods? (expected: ${exp_create[$i]})\n"
  oc auth can-i create pods || true

  printf "===> get pods? (expected: ${exp_do_get[$i]})\n"
  oc get pods || true

  printf "===> create pods? (expected: ${exp_do_create[$i]})\n"
  oc run nginx --image=nginx --port=80 --restart=Never || true
done
