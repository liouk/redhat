#!/bin/bash

# Script to deploy Keycloak for OIDC testing in OpenShift
# Based on test/library/keycloakidp.go and test/library/idpdeployment.go

set -euo pipefail

# Configuration
NAMESPACE_PREFIX="${NAMESPACE_PREFIX:-oidc-keycloak}"
KEYCLOAK_IMAGE="${KEYCLOAK_IMAGE:-quay.io/keycloak/keycloak:25.0}"
KEYCLOAK_ADMIN="${KEYCLOAK_ADMIN:-admin}"
KEYCLOAK_ADMIN_PASSWORD="${KEYCLOAK_ADMIN_PASSWORD:-password}"

# Generate a unique namespace name
NAMESPACE="${NAMESPACE_PREFIX}-$(date +%s)"

echo "Deploying Keycloak to namespace: ${NAMESPACE}"

# Create namespace with labels
cat <<EOF | oc apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: ${NAMESPACE}
  labels:
    pod-security.kubernetes.io/enforce: privileged
    pod-security.kubernetes.io/audit: privileged
    pod-security.kubernetes.io/warn: privileged
    security.openshift.io/scc.podSecurityLabelSync: "false"
EOF

# Create ServiceAccount
cat <<EOF | oc apply -n ${NAMESPACE} -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: keycloak
EOF

# Create RoleBinding for privileged SCC
cat <<EOF | oc apply -n ${NAMESPACE} -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: privileged-scc-to-default-sa
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:openshift:scc:privileged
subjects:
- kind: ServiceAccount
  name: keycloak
  namespace: ${NAMESPACE}
EOF

# Create Service (with annotation to generate serving cert)
cat <<EOF | oc apply -n ${NAMESPACE} -f -
apiVersion: v1
kind: Service
metadata:
  name: pod-svc
  annotations:
    service.beta.openshift.io/serving-cert-secret-name: serving-secret
spec:
  selector:
    app: oidc-keycloak-app
  ports:
  - name: https
    port: 8443
    targetPort: 8443
  - name: http
    port: 8080
    targetPort: 8080
EOF

# Wait for serving cert secret to be created
echo "Waiting for serving cert secret to be created..."
for i in {1..30}; do
  if oc get secret serving-secret -n ${NAMESPACE} &>/dev/null; then
    echo "Serving cert secret created"
    break
  fi
  if [ $i -eq 30 ]; then
    echo "Timeout waiting for serving cert secret"
    exit 1
  fi
  sleep 2
done

# Create Deployment
cat <<EOF | oc apply -n ${NAMESPACE} -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: keycloak
  labels:
    app: oidc-keycloak-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: oidc-keycloak-app
  template:
    metadata:
      name: keycloak
      labels:
        app: oidc-keycloak-app
    spec:
      serviceAccountName: keycloak
      containers:
      - name: payload
        image: ${KEYCLOAK_IMAGE}
        securityContext:
          privileged: true
        ports:
        - containerPort: 8443
        - containerPort: 8080
        - containerPort: 9000
        env:
        - name: KEYCLOAK_ADMIN
          value: "${KEYCLOAK_ADMIN}"
        - name: KEYCLOAK_ADMIN_PASSWORD
          value: "${KEYCLOAK_ADMIN_PASSWORD}"
        - name: KC_HEALTH_ENABLED
          value: "true"
        - name: KC_HOSTNAME_STRICT
          value: "false"
        - name: KC_PROXY
          value: "reencrypt"
        - name: KC_HTTPS_CERTIFICATE_FILE
          value: /etc/x509/https/tls.crt
        - name: KC_HTTPS_CERTIFICATE_KEY_FILE
          value: /etc/x509/https/tls.key
        command:
        - /opt/keycloak/bin/kc.sh
        - start-dev
        volumeMounts:
        - name: certkeypair
          mountPath: /etc/x509/https
          readOnly: true
        resources:
          requests:
            cpu: 500m
            memory: 700Mi
        readinessProbe:
          httpGet:
            path: /health/ready
            port: 9000
            scheme: HTTPS
          initialDelaySeconds: 10
        livenessProbe:
          httpGet:
            path: /health/live
            port: 9000
            scheme: HTTPS
          initialDelaySeconds: 10
      volumes:
      - name: certkeypair
        secret:
          secretName: serving-secret
EOF

# Wait for deployment to be ready
echo "Waiting for Keycloak deployment to be ready..."
oc wait --for=condition=available --timeout=600s deployment/keycloak -n ${NAMESPACE}

# Create Route
cat <<EOF | oc apply -n ${NAMESPACE} -f -
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: test-route
spec:
  tls:
    termination: reencrypt
    insecureEdgeTerminationPolicy: Redirect
  to:
    kind: Service
    name: pod-svc
  port:
    targetPort: https
EOF

# Wait for route to be admitted
echo "Waiting for route to be admitted..."
for i in {1..30}; do
  ROUTE_HOST=$(oc get route test-route -n ${NAMESPACE} -o jsonpath='{.status.ingress[0].host}' 2>/dev/null || true)
  if [ -n "${ROUTE_HOST}" ]; then
    echo "Route admitted at: ${ROUTE_HOST}"
    break
  fi
  if [ $i -eq 30 ]; then
    echo "Timeout waiting for route to be admitted"
    exit 1
  fi
  sleep 2
done

# Create CA ConfigMap in openshift-config namespace
# This syncs the default ingress CA so the OIDC provider can trust the Keycloak route
echo "Creating CA ConfigMap in openshift-config namespace..."
CA_BUNDLE=$(oc get configmap default-ingress-cert -n openshift-config-managed -o jsonpath='{.data.ca-bundle\.crt}')
cat <<EOF | oc apply -n openshift-config -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: keycloak-${NAMESPACE}-ca
data:
  ca.crt: |
$(echo "${CA_BUNDLE}" | sed 's/^/    /')
EOF
echo "CA ConfigMap created: keycloak-${NAMESPACE}-ca"

# Create dummy client secret for console in openshift-console namespace
echo "Creating dummy client secret in openshift-console namespace..."
cat <<EOF | oc apply -n openshift-console -f -
apiVersion: v1
kind: Secret
metadata:
  name: openshift-console-oidc-client-secret
type: Opaque
stringData:
  clientSecret: "REPLACE_WITH_ACTUAL_CLIENT_SECRET_FROM_KEYCLOAK"
EOF
echo "Dummy client secret created (needs to be updated with actual secret from Keycloak)"

# Display deployment information
echo ""
echo "========================================="
echo "Keycloak deployed successfully!"
echo "========================================="
echo "Namespace: ${NAMESPACE}"
echo "Keycloak URL: https://${ROUTE_HOST}/realms/master"
echo "Admin Console: https://${ROUTE_HOST}"
echo "Admin Username: ${KEYCLOAK_ADMIN}"
echo "Admin Password: ${KEYCLOAK_ADMIN_PASSWORD}"
echo ""
echo "To clean up, run:"
echo "  oc delete namespace ${NAMESPACE}"
echo ""

# Offer to create OIDC authentication configuration
echo "========================================="
read -p "Do you want to generate an OIDC Authentication/cluster manifest? (yes/no/skip): " GENERATE_AUTH

if [[ "${GENERATE_AUTH}" == "yes" || "${GENERATE_AUTH}" == "y" ]]; then
  # Generate the Authentication manifest
  AUTH_MANIFEST="/tmp/authentication-oidc-${NAMESPACE}.yaml"

  cat <<EOF > ${AUTH_MANIFEST}
apiVersion: config.openshift.io/v1
kind: Authentication
metadata:
  name: cluster
spec:
  oauthMetadata:
    name: ""
  serviceAccountIssuer: ""
  type: "OIDC"
  webhookTokenAuthenticator:
  oidcProviders:
  - name: "keycloak-${NAMESPACE}"
    issuer:
      issuerURL: "https://${ROUTE_HOST}/realms/master"
      audiences:
      - openshift-aud
      - admin-cli
      issuerCertificateAuthority:
        name: keycloak-${NAMESPACE}-ca
    oidcClients:
    - componentName: "console"
      componentNamespace: "openshift-console"
      clientID: "console"
      clientSecret:
        name: openshift-console-oidc-client-secret
    claimMappings:
      username:
        claim: "email"
        prefixPolicy: "Prefix"
        prefix:
          prefixString: "keycloak:"
      groups:
        claim: "groups"
        prefix: ""
EOF

  echo ""
  echo "Authentication manifest generated at: ${AUTH_MANIFEST}"
  echo ""
  cat ${AUTH_MANIFEST}
  echo ""

  read -p "Do you want to apply this manifest now? (yes/no): " APPLY_AUTH

  if [[ "${APPLY_AUTH}" == "yes" || "${APPLY_AUTH}" == "y" ]]; then
    echo "Applying Authentication manifest..."
    oc apply -f ${AUTH_MANIFEST}
    echo ""
    echo "Authentication configuration applied!"
    echo ""
    echo "Monitor rollout by running"
    echo "  watch 'oc get kubeapiserver cluster -ojsonpath=\"{.status.nodeStatuses}\"|jq'"
    echo ""
    echo "IMPORTANT: Before this configuration will work, you need to:"
    echo "1. Create a client named 'console' in Keycloak with appropriate redirect URIs"
    echo "2. Update the Secret 'openshift-console-oidc-client-secret' in openshift-console namespace"
    echo "   with the actual client secret from Keycloak:"
    echo "   oc patch secret openshift-console-oidc-client-secret -n openshift-console \\"
    echo "     --type merge -p '{\"stringData\":{\"clientSecret\":\"YOUR_ACTUAL_SECRET\"}}'"
    echo ""
    echo "Note: The CA ConfigMap and a dummy client Secret have already been created"
  else
    echo "Manifest saved but not applied. You can apply it later with:"
    echo "  oc apply -f ${AUTH_MANIFEST}"
    echo ""
    echo "Note: The CA ConfigMap and a dummy client Secret have already been created"
    echo "      You'll need to create the console client in Keycloak and update the Secret"
  fi
else
  echo "Skipping Authentication manifest generation."
fi
echo ""
