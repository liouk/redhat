#!/usr/bin/env bash

client_cert=$1
client_key=$2
server_cert=$3 # get from the server: openssl s_client -showcerts -connect your_server_url:port

oauth_url=$(oc -n openshift-authentication get route oauth-openshift -o jsonpath='{.spec.host}')

echo "quit" | openssl s_client -connect "$oauth_url:443" -cert $client_cert -key $client_key -CAfile $server_cert
