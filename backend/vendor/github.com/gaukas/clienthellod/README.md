# clienthellod
![Go Build Status](https://github.com/gaukas/clienthellod/actions/workflows/go.yml/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/gaukas/clienthellod)](https://goreportcard.com/report/github.com/gaukas/clienthellod)
[![DeepSource](https://app.deepsource.com/gh/gaukas/clienthellod.svg/?label=active+issues&show_trend=true&token=GugDSBnYAxAF25QNpfyAO5d2)](https://app.deepsource.com/gh/gaukas/clienthellod/)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fgaukas%2Fclienthellod.svg?type=shield&issueType=license)](https://app.fossa.com/projects/git%2Bgithub.com%2Fgaukas%2Fclienthellod?ref=badge_shield&issueType=license)

ClientHello Parser/Resolver as a Service from [tlsfingerprint.io](https://tlsfingerprint.io).

## What does it do

`clienthellod`, read as "client hello DEE", is a service that parses and resolves the ClientHello message sent by the client to the server. It is a part of the TLS fingerprintability research project which spans [tlsfingerprint.io](https://tlsfingerprint.io) and [quic.tlsfingerprint.io](https://quic.tlsfingerprint.io). It parses the ClientHello messages sent by TLS clients and QUIC Client Initial Packets sent by QUIC clients and display the parsed information in a human-readable format with high programmability. 

See [tlsfingerprint.io](https://tlsfingerprint.io) and [quic.tlsfingerprint.io](https://quic.tlsfingerprint.io) for more details about the project.

## How to use

`clienthellod` is provided as a Go library in the root directory of this repository. 

### Quick Start

#### TLS ClientHello

```go
    tcpLis, err := net.Listen("tcp", ":443")
    defer tcpLis.Close()

    conn, err := tcpLis.Accept()
	if err != nil {
        panic(err)
	}
    defer conn.Close()

	ch, err := clienthellod.ReadClientHello(conn) // saves ClientHello
    if err != nil {
        panic(err)
    }

    err := ch.ParseClientHello() // parses ClientHello's fields
    if err != nil {
        panic(err)
    }

    jsonB, err = json.MarshalIndent(ch, "", "  ")
    if err != nil {
        panic(err)
    }

    fmt.Println(string(jsonB))
    fmt.Println("ClientHello ID: " + ch.FingerprintID(false)) // prints ClientHello's original fingerprint ID, as TLS extension IDs in their provided order
    fmt.Println("ClientHello NormID: " + ch.FingerprintID(true)) // prints ClientHello's normalized fingerprint ID, as TLS extension IDs in a sorted order
```

#### QUIC Client Initial Packet

```go
    udpConn, err := net.ListenUDP("udp", ":443")
    defer udpConn.Close()

    buf := make([]byte, 65535)
    n, addr, err := udpConn.ReadFromUDP(buf)
    if err != nil {
        panic(err)
    }

    cip, err := clienthellod.ParseQUICCIP(buf[:n]) // reads in and parses QUIC Client Initial Packet
    if err != nil {
        panic(err)    
    }

    jsonB, err = json.MarshalIndent(cip, "", "  ")
    if err != nil {
        panic(err)
    }

    fmt.Println(string(jsonB)) // including fingerprint IDs of: ClientInitialPacket, QUIC Header, QUIC ClientHello, QUIC Transport Parameters' combination
```

#### Use with Caddy

`clienthellod` is also provided as a Caddy plugin, `modcaddy`, which can be used to capture ClientHello messages and QUIC Client Initial Packets. See Section [modcaddy](#modcaddy) for more details.

## modcaddy

`modcaddy` is a Caddy plugin that provides:
- An caddy `app` that can be used to temporarily store captured ClientHello messages and QUIC Client Initial Packets. 
- A caddy `handler` that can be used to serve the ClientHello messages and QUIC Client Initial Packets to the client sending the request. 
- A caddy `listener` that can be used to capture ClientHello messages and QUIC Client Initial Packets.

You will need to use [xcaddy](https://github.com/caddyserver/xcaddy) to rebuild Caddy with `modcaddy` included.

It is worth noting that some web browsers may not choose to switch to QUIC protocol in localhost environment, which may result in the QUIC Client Initial Packet not being sent and therefore not being captured/analyzed.

### Build 

```bash
xcaddy build --with github.com/gaukas/clienthellod/modcaddy
```

#### When build locally with changes 

```bash
xcaddy build --with github.com/gaukas/clienthellod/modcaddy --with github.com/gaukas/clienthellod/=./
```

### Caddyfile

A sample Caddyfile is provided below.

```Caddyfile
{      
    # debug # for debugging purpose
    # https_port   443 # currently, QUIC listener works only on port 443, otherwise you need to make changes to the code
    order clienthellod before file_server # make sure it hits handler before file_server
    clienthellod { # app (reservoir)
        validfor 120s 30s # params: validFor [cleanEvery] # increased for QUIC
    }
    servers {
        listener_wrappers {
            clienthellod { # listener
                tcp # listens for TCP and saves TLS ClientHello 
                udp # listens for UDP and saves QUIC Client Initial Packet
            }
            tls
        }
        # protocols h3
    }
}

1.mydomain.com {
    # tls internal
    clienthellod { # handler
        # quic # mutually exclusive with tls
        tls # listener_wrappers.clienthellod.tcp must be set
    }
    file_server {
        root /var/www/html
    }
}

2.mydomain.com {
    # tls internal
    clienthellod { # handler
        quic # listener_wrappers.clienthellod.udp must be set
        # tls # mutually exclusive with quic
    }
    file_server {
        root /var/www/html
    }
}
```

## License

This project is developed and distributed under Apache-2.0 license. 

[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fgaukas%2Fclienthellod.svg?type=large&issueType=license)](https://app.fossa.com/projects/git%2Bgithub.com%2Fgaukas%2Fclienthellod?ref=badge_large&issueType=license)
