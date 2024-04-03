#!/usr/bin/env bash

naptime=10
while true; do
	date
	oc get oauth cluster -oyaml | grep -q "test-request-header"
	if [ $? -eq 0 ]; then
    naptime=1
		echo "test idp found; check certs"
		oauth_url="$(oc -n openshift-authentication get route oauth-openshift -o jsonpath='{.spec.host}'):443"
		echo "quit" | openssl s_client -connect "$oauth_url" > openssl_connect.txt 2>/dev/null
		grep -q "Testing CA" openssl_connect.txt
		if [ $? -eq 0 ]; then
			echo "Testing CA found in acceptable client certificate CA names"
      oc -n openshift-authentication get pods
    else
      echo "Testing CA not found in acceptable client certificate CA names yet"
		fi
	else
		echo "test-request-header idp not found yet"
    if [ "$naptime" == "2" ]; then
      echo "test cleaned up; exit"
      exit
    fi
	fi

	echo
	sleep $naptime
done
