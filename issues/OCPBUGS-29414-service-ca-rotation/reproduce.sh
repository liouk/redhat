#!/usr/bin/env bash

# delete signing-key to cause a fresh start
oc delete secret -n openshift-service-ca signing-key
oc delete cm -n openshift-service-ca signing-cabundle

# grab original CA cert
oc get cm -n openshift-service-ca signing-cabundle -otemplate='{{ index .data "ca-bundle.crt" }}' > original.ca.crt

# grab original serving cert
oc get secret -n openshift-authentication v4-0-config-system-serving-cert -otemplate='{{ index .data "tls.crt" }}' | base64 -d > original.cert.crt

# force a rotation
# .spec.unsupportedConfigOverrides:
#   forceRotation:
#     reason: some_reason
oc edit serviceca

# grab new CA cert; must wait for two certs in it
oc exec -n openshift-authentication $(oc get pods -n openshift-authentication --no-headers | head -1 | tail -1 | cut -d' ' -f1) -- cat /var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt > new.ca.crt

# grab new serving cert
oc get secret -n openshift-authentication v4-0-config-system-serving-cert -otemplate='{{ index .data "tls.crt" }}' | base64 -d > new.cert.crt
