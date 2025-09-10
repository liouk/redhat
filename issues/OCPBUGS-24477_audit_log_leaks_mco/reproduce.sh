#!/usr/bin/env bash

# set audit log level to WriteRequestBodies
oc patch apiserver cluster --type=merge -p '{"spec":{"audit":{"profile":"WriteRequestBodies"}}}'

# wait for rollout (check `oc get co`)
sleep 60

# update some file that will rollout MachineConfigPool
oc apply -f mc.yaml

# search for the leaked secret in the logs
oc adm node-logs --role=master --path=kube-apiserver/audit.log | grep -i cHJhbW9kcmVkaGF0Cg==
