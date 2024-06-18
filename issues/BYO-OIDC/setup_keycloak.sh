#!/usr/bin/env bash
set -e

function get_authn_crd_gen() {
    echo "$(oc get crd authentications.config.openshift.io -otemplate='{{ .metadata.generation }}')"
}

# run from the cluster-authentication-operator repo
OPENSHIFT_KEEP_IDP=true WHAT=TestKeycloakAsOIDCPasswordGrantCheckAndGroupSync make run-e2e-test

KC_NS="$(oc get ns -l'e2e-test=openshift-authentication-operator' --no-headers | head -1 | cut -d' ' -f1)"
KC_URL="https://$(oc get route -n $KC_NS test-route --template='{{ .spec.host }}')/realms/master"

authnCRDGen="$(get_authn_crd_gen)"

# get the new OIDC API
oc patch featuregate cluster -p '{"spec":{"featureSet":"TechPreviewNoUpgrade"}}' --type=merge

# wait for the CRD to be patched
while [[ "$(get_authn_crd_gen)" -le "$authnCRDGen" ]]; do echo "waiting for Authentication CRD to be updated"; sleep 5s; done

oc patch cm -n openshift-config-managed default-ingress-cert -p '{"metadata":{"namespace":"openshift-config"}}' --dry-run=client -o yaml | oc apply -f -
oc patch proxy cluster -p '{"spec":{"trustedCA":{"name":"default-ingress-cert"}}}' --type=merge

oc patch kubeapiserver cluster -p '{"spec":{"unsupportedConfigOverrides":{"apiServerArguments":{"oidc-ca-file":["/etc/kubernetes/static-pod-certs/configmaps/trusted-ca-bundle/ca-bundle.crt"],"oidc-client-id":["openshift-aud"], "oidc-issuer-url":["'"${KC_URL}"'"], "oidc-groups-claim":["groups"],"oidc-username-claim":["email"],"oidc-username-prefix":["-"]}}}}' --type=merge

# TODO: wait for the openshift-console/console client appears in authentication.config/cluster .status.oidcClients

oc patch authentication cluster -p '{"spec":{"type":"OIDC","webhookTokenAuthenticator":null,"oidcProviders":[{"name":"keycloak","issuer":{"audiences":["openshift-aud"],"issuerURL":"'${KC_URL}'"},"claimMappings":{"groups":{"claim":"groups"},"username":{"claim":"email"}},"oidcClients":[{"clientID":"console","clientSecret":{"name":"console-secret"},"componentName":"console","componentNamespace":"openshift-console"}]}]}}' --type=merge

# this should get us a token
curl -k "$KC_URL/protocol/openid-connect/token" -d "grant_type=password" -d "client_id=admin-cli" -d "scope=openid" -d "username=admin" -d "password=password" | jq -r '.id_token'

echo
echo "The Authentication spec is now set up. Go to $KC_URL to create the client scope that will attach the 'openshift-aud' to all clients by default, then create an authenticated client named 'console'."
echo "After that, copy its client secret and create a Secret object like so: 'oc create secret generic -n openshift-config console-secret --from-literal=clientSecret=<your_secret_here>'"
echo
echo "You'll also want to create a user with an email setup and verified"
echo "Don't forget to disable refresh token reuse in the realm settings"
