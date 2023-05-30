#!/usr/bin/env bash

# step 1: unmanage CAO from the CVO
(cd $RH_SCRIPTS_DIR; ./cvo-unmanage.sh {openshift-,}authentication-operator true)

# step 2: patch CAO with new image
read -p "cluster-authentication-operator image URL: " CAO_IMG
(cd $RH_SCRIPTS_DIR; ./patch-img.sh cluster-authentication-operator "$CAO_IMG")
sleep 5
oc -n openshift-authentication-operator get pods

read -p "Press enter to continue with oauth-server deployment"

# step 3: patch OAuthServer with new image
read -p "oauth-server image URL: " OAUTH_SERVER_IMG
(cd $RH_SCRIPTS_DIR; ./patch-img.sh oauth-server "$OAUTH_SERVER_IMG")
sleep 5
oc -n openshift-authentication get pods
