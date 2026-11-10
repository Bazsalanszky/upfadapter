// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package udp

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"
)

// A dual-stack adapter binds the wildcard and talks to an IPv4 and an IPv6 user plane from one
// socket. A response used to be looked up under the address Run asked to bind (":8805"), not
// the socket's own ("[::]:8805"), found no transaction and was dropped; so it was for any
// adapter without an IPv4 POD_IP, which bound the wildcard too.
func TestAWildcardSocketMatchesResponsesFromBothFamilies(t *testing.T) {
	if probe, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback}); err != nil {
		t.Skipf("no IPv6 loopback here: %v", err)
	} else {
		probe.Close()
	}

	conn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	local := conn.LocalAddr().(*net.UDPAddr)

	previous := Server
	// Addr as Run sets it for a dual-stack adapter: what was asked for, not what was bound.
	Server = &PfcpServer{Addr: &net.UDPAddr{Port: local.Port}, Conn: conn}
	responses := make(chan message.Message, 2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		serve(ctx, conn, func(msg message.Message, _ *net.UDPAddr) { responses <- msg })
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		Server = previous
	})

	for i, peerIP := range []string{"127.0.0.1", "::1"} {
		peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(peerIP)})
		if err != nil {
			t.Fatalf("standing in for the UPF on %s: %v", peerIP, err)
		}
		defer peer.Close()

		seq := uint32(0x700 + i)
		req := message.NewHeartbeatRequest(seq, ie.NewRecoveryTimeStamp(time.Now()), nil)
		if err = SendPfcp(req, peer.LocalAddr().(*net.UDPAddr), nil); err != nil {
			t.Fatalf("sending to %s: %v", peerIP, err)
		}

		if err = peer.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 1024)
		n, from, err := peer.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("UPF %s received nothing: %v", peerIP, err)
		}
		if _, err := message.Parse(buf[:n]); err != nil {
			t.Fatalf("UPF %s: %v", peerIP, err)
		}
		rsp := message.NewHeartbeatResponse(seq, ie.NewRecoveryTimeStamp(time.Now()))
		out := make([]byte, rsp.MarshalLen())
		if err := rsp.MarshalTo(out); err != nil {
			t.Fatal(err)
		}
		if _, err := peer.WriteToUDP(out, from); err != nil {
			t.Fatalf("UPF %s answering %v: %v", peerIP, from, err)
		}

		select {
		case msg := <-responses:
			if msg.MessageType() != message.MsgTypeHeartbeatResponse || msg.Sequence() != seq {
				t.Errorf("dispatched %s seq %d, want the heartbeat response seq %d", msg.MessageTypeName(), msg.Sequence(), seq)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("the response from %s was not dispatched: it matched no transaction", peerIP)
		}

		// Wait for the transaction to be retired, so its goroutine is done with Server before the
		// cleanup puts the previous one back.
		deadline := time.Now().Add(3 * time.Second)
		for {
			table, ok := Server.ConsumerTable.Load(local.String())
			if !ok {
				break
			}
			if _, held := table.Load(seq); !held {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("transaction %d was never retired", seq)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}
