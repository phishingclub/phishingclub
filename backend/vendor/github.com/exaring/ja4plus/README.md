[![Tests](https://github.com/exaring/ja4plus/actions/workflows/ci.yml/badge.svg)](https://github.com/exaring/ja4plus/actions/workflows/main.yaml)
[![Docs](https://pkg.go.dev/badge/github.com/exaring/ja4plus.svg)](https://pkg.go.dev/github.com/exaring/ja4plus)
[![Report Card](https://goreportcard.com/badge/github.com/exaring/ja4plus)](https://goreportcard.com/report/github.com/exaring/ja4plus)

# JA4Plus

<img src="logo.png" alt="ja4plus logo" width="200pt"/>

JA4Plus is a go library for generating [ja4+ fingerprints](https://github.com/FoxIO-LLC/ja4).

## Overview

JA4Plus currently offers a single fingerprinting function:
- **JA4**: Fingerprint based on [TLS ClientHello](https://pkg.go.dev/crypto/tls#ClientHelloInfo) information.

Contributions are welcome for the other fingerprints in the family 😉

### Omission of JA4H

The JA4H hash, based on properties of the HTTP request, cannot currently be easily implemented in go, since it requires
headers to be observed in the order sent by the client. See e.g.: https://go.dev/issue/24375

## Examples

For example usage, checkou out [examples_test.go](./examples_test.go).