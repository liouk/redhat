#!/usr/bin/env python
import itertools
import tempfile
import subprocess
import time
import signal
import os

from functools import reduce


# Situation - cross-signed certs
#
# ca.pem      2.pem ------|            <- CAs, ca.pem is old, 2.pem is new
# (priv A)   (priv B)     |
#   |   \                 |
#   |    \                |
#   |     \               |
#   |      \              |
#  [  ]      3.pem        |
#  [  ]      (priv B)     |
#               |         |
#               |         |
#             serving bundle
#
#
#
#             1.pem is the server cert, signed by priv key of 2 (same as 3)
noServerBundleCA = ["2", "3"]
serverBundleCA = noServerBundleCA + ["ca"]
dirname = "openssl-test-bundles"

def concat(a, b):
    return a + b

def testCertPermutations(certfileList):
    servingCert = ''
    with open('1.pem', 'r') as servercert:
        servingCert = servercert.read()

    for nameperm in itertools.permutations(certfileList):
        # tempDir = tempfile.TemporaryDirectory(prefix="openssl-tests")
        contents = [servingCert]
        for fname in nameperm:
            with open(fname + ".pem", 'r') as certfile:
                contents.append(certfile.read())
    
        servcertname = ''
        with open(dirname + '/' + '1' + reduce(concat, nameperm) + '.pem', 'w') as serving:
            servcertname = serving.name
            for c in contents:
                serving.write(c)

        print('starting server with combination ' + servcertname)
        server = subprocess.Popen(
            ['/home/ilias/redhat/issues/OCPBUGS-29414-service-ca-rotation/server', servcertname, 'key.pem'],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        client = subprocess.Popen(
            ['openssl', 's_client', '-connect', 'localhost:45011', '-servername', 'myservice.mynamespace.svc', '-CAfile', 'ca.pem', '-showcerts'],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        out, err = client.communicate('quit\n')
        client.wait()

        if client.returncode != 0:
            print('command failed:\n', err)
        else:
            print(err, "-------", out)

        server.send_signal(signal.SIGINT)
        server.wait()

if __name__ == '__main__':
    if not os.path.exists(dirname):
        os.makedirs(dirname)
    testCertPermutations(noServerBundleCA)
    testCertPermutations(serverBundleCA)
