#!/usr/bin/env bash

echo "Checking existence of 'configs.imageregistry.operator.openshift.io'"
oc get configs.imageregistry.operator.openshift.io

oauth_route=$(oc get route -n openshift-authentication oauth-openshift -o jsonpath='{.spec.host}')
echo -e "\nOAuth route: $oauth_route"

redirect_uri="https://$oauth_route/oauth/token/display"
echo -e "\nRedirect URI: $redirect_uri"

prefix="oauth-tokens-test"
echo -e "\nDeleting all '$prefix-*' namespaces"
oc get namespaces | grep $prefix | awk '{print $1}' | xargs -I {} oc delete namespace {}

suffix=$(echo $RANDOM | md5sum | head -c 5)
ns="$prefix-$suffix"
echo -e "\nCreating new test ns"
oc create ns $ns

sa="${prefix}-sa"
echo -e "\nCreating new SA"
oc -n $ns create sa "$sa"

anno_redirecturi="serviceaccounts.openshift.io/oauth-redirecturi.first=$redirect_uri"
echo -e "\nAnnotating SA with: '$anno_redirecturi'"
oc -n $ns annotate sa $sa "$anno_redirecturi"

anno_challenges="serviceaccounts.openshift.io/oauth-want-challenges=false"
echo -e "\nAnnotating SA with: '$anno_challenges'"
oc -n $ns annotate sa $sa "$anno_challenges"

echo
oc -n $ns get sa $sa -o jsonpath='{.metadata.annotations}' | jq

browser_url="https://$oauth_route/oauth/authorize?client_id=system:serviceaccount:$ns:$sa&redirect_uri=$redirect_uri&response_type=code&scope=user:info"
echo -e "\nOpen URL to authenticate:\n$browser_url"

echo
read -p "Hit enter after opening the link above: " yn

echo -e "\noauth-server errors:"
for pod in $(oc -n openshift-authentication get pods --no-headers -ocustom-columns="NAME:.metadata.name"); do echo $pod; oc -n openshift-authentication logs $pod | grep "$ns"; done
