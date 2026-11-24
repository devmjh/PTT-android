// turn.go
package main

import (
	"fmt"
	"net"

	"github.com/pion/turn/v5"
)

// Embedded TURN is scoped to the media relay. Port 3478 carries client
// allocations over either UDP or TCP. The relayed media remains DTLS-SRTP.
func startTURN(bind, ip, secret string) (*turn.Server, error) {
	addr := net.ParseIP(ip)
	if addr.To4() == nil || len(secret) < 32 {
		return nil, fmt.Errorf("embedded TURN needs RIDGE_PUBLIC_IP and a secret of at least 32 characters")
	}
	tcp, e := net.Listen("tcp4", bind)
	if e != nil {
		return nil, e
	}
	udp, e := net.ListenPacket("udp4", tcp.Addr().String())
	if e != nil {
		tcp.Close()
		return nil, e
	}
	makeGenerator := func() turn.RelayAddressGenerator {
		return &turn.RelayAddressGeneratorPortRange{RelayAddress: addr, Address: "0.0.0.0", MinPort: 50000, MaxPort: 50100}
	}
	allow := func(_ net.Addr, peer net.IP) bool { return peer.Equal(addr) }
	srv, e := turn.NewServer(turn.ServerConfig{Realm: "ridge-radio", AuthHandler: turn.LongTermTURNRESTAuthHandler(secret, nil),
		PacketConnConfigs: []turn.PacketConnConfig{{PacketConn: udp, RelayAddressGenerator: makeGenerator(), PermissionHandler: allow}},
		ListenerConfigs:   []turn.ListenerConfig{{Listener: tcp, RelayAddressGenerator: makeGenerator(), PermissionHandler: allow}}})
	if e != nil {
		tcp.Close()
		udp.Close()
	}
	return srv, e
}
