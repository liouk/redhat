#!/usr/bin/env bash

serving_bundle="$1"
csplit -s -z -n 1 -f '' -b '%d.pem' $serving_bundle '/-----BEGIN CERTIFICATE-----/' '{*}'

# see standa.py for numbering
mv 2.pem 3.pem && mv 1.pem 2.pem && mv 0.pem 1.pem

./standa.py | rg "^starting server|^Verify return code:"
