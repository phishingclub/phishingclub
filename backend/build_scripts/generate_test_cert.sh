#!/bin/bash
# generate private key
openssl genrsa -out test.key 2048
# generate self-signed certificate
# generate certificate
openssl req -new -x509 -key test.key -out test.pem -days 365
