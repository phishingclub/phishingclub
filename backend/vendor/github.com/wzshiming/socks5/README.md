# socks5

Socks5/Socks5h server and client

[![Build](https://github.com/wzshiming/socks5/actions/workflows/go-cross-build.yml/badge.svg)](https://github.com/wzshiming/socks5/actions/workflows/go-cross-build.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/wzshiming/socks5)](https://goreportcard.com/report/github.com/wzshiming/socks5)
[![GoDoc](https://pkg.go.dev/badge/github.com/wzshiming/socks5)](https://pkg.go.dev/github.com/wzshiming/socks5)
[![GitHub license](https://img.shields.io/github/license/wzshiming/socks5.svg)](https://github.com/wzshiming/socks5/blob/master/LICENSE)

This project is to add protocol support for the [Bridge](https://github.com/wzshiming/bridge), or it can be used alone

The following is the implementation of other proxy protocols

- [Socks4](https://github.com/wzshiming/socks4)
- [HTTP Proxy](https://github.com/wzshiming/httpproxy)
- [Shadow Socks](https://github.com/wzshiming/shadowsocks)
- [SSH Proxy](https://github.com/wzshiming/sshproxy)
- [Any Proxy](https://github.com/wzshiming/anyproxy)
- [Emux](https://github.com/wzshiming/emux)

## Usage

[API Documentation](https://godoc.org/github.com/wzshiming/socks5)

[Example](https://github.com/wzshiming/socks5/blob/master/cmd/socks5/main.go)

- [x] Support for the CONNECT command
- [x] Support for the BIND command
- [x] Support for the ASSOCIATE command

## License

Licensed under the MIT License. See [LICENSE](https://github.com/wzshiming/socks5/blob/master/LICENSE) for the full license text.
