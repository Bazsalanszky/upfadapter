// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/omec-project/upfadapter/config"
	"github.com/omec-project/upfadapter/pfcp/udp"
	"github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"
)

// peerSocket is a user plane's PFCP socket, for the adapter to answer.
func peerSocket(t *testing.T) *net.UDPConn {
	t.Helper()

	packet, err := (&net.ListenConfig{}).ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	conn, ok := packet.(*net.UDPConn)
	if !ok {
		t.Fatalf("listener is %T, want *net.UDPConn", packet)
	}

	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Logf("closing the peer socket: %v", err)
		}
	})

	return conn
}

// readHeartbeatResponse reads the adapter's answer on the peer's socket.
func readHeartbeatResponse(t *testing.T, conn *net.UDPConn) *message.HeartbeatResponse {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("setting the read deadline: %v", err)
	}

	buf := make([]byte, 1500)

	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("no heartbeat response reached the user plane: %v", err)
	}

	rsp, err := message.ParseHeartbeatResponse(buf[:n])
	if err != nil {
		t.Fatalf("the answer is not a heartbeat response: %v", err)
	}

	return rsp
}

// A user plane that heartbeats the adapter has to be answered, under its own sequence number and
// with the adapter's Recovery Time Stamp. Open5GS heartbeats from the moment it is associated and
// releases the association after four unanswered requests; the adapter answered none, so every
// association through it ended 7.5 seconds after it was set up.
func TestAHeartbeatRequestFromTheUserPlaneIsAnswered(t *testing.T) {
	listeningAdapter(t)

	upf := peerSocket(t)
	upfAddr, ok := upf.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("peer address is %T", upf.LocalAddr())
	}

	upfStart := time.Now().Add(-time.Hour)
	HandlePfcpHeartbeatRequest(message.NewHeartbeatRequest(0x123456, ie.NewRecoveryTimeStamp(upfStart), nil), upfAddr)

	rsp := readHeartbeatResponse(t, upf)
	if rsp.Sequence() != 0x123456 {
		t.Errorf("response sequence %#x, want the request's %#x", rsp.Sequence(), 0x123456)
	}

	if rsp.RecoveryTimeStamp == nil {
		t.Fatal("the response has no Recovery Time Stamp, which TS 29.244 clause 7.4.2.2 makes mandatory")
	}

	ts, err := rsp.RecoveryTimeStamp.RecoveryTimeStamp()
	if err != nil {
		t.Fatalf("decoding the Recovery Time Stamp: %v", err)
	}

	if d := config.UpfServerStartTime.Sub(ts); d < 0 || d >= time.Second {
		t.Errorf("Recovery Time Stamp %v, want the adapter's start time %v", ts.UTC(), config.UpfServerStartTime.UTC())
	}

	// Answered, not remembered: no response transaction is held for the peer.
	if _, held := udp.Server.ConsumerTable.Load(upfAddr.String()); held {
		t.Error("a transaction table is held for the user plane after answering its heartbeat")
	}
}

// A retransmitted heartbeat is answered again: the adapter keeps no state that could absorb it.
func TestARetransmittedHeartbeatRequestIsAnsweredAgain(t *testing.T) {
	listeningAdapter(t)

	upf := peerSocket(t)
	upfAddr, ok := upf.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("peer address is %T", upf.LocalAddr())
	}

	req := message.NewHeartbeatRequest(77, ie.NewRecoveryTimeStamp(time.Now()), nil)
	for range 2 {
		HandlePfcpHeartbeatRequest(req, upfAddr)
		if rsp := readHeartbeatResponse(t, upf); rsp.Sequence() != 77 {
			t.Errorf("response sequence %d, want 77", rsp.Sequence())
		}
	}
}
