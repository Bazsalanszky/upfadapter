// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package pfcp

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/omec-project/upfadapter/pfcp/udp"
	"github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"
)

func listenUDP(t *testing.T) *net.UDPConn {
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
			t.Logf("closing: %v", err)
		}
	})

	return conn
}

// The dispatcher used to drop a Heartbeat Request as an unknown message type ("unknown pfcp
// message type [1]: Heartbeat Request"): its case was commented out.
func TestTheDispatcherAnswersAHeartbeatRequest(t *testing.T) {
	adapter := listenUDP(t)
	upf := listenUDP(t)

	// Put back what the other tests in this package expect: no server, so a send fails.
	previous := udp.Server
	udp.Server = &udp.PfcpServer{Addr: adapter.LocalAddr().(*net.UDPAddr), Conn: adapter}
	t.Cleanup(func() { udp.Server = previous })

	Dispatch(message.NewHeartbeatRequest(9, ie.NewRecoveryTimeStamp(time.Now()), nil), upf.LocalAddr().(*net.UDPAddr))

	if err := upf.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("setting the read deadline: %v", err)
	}

	buf := make([]byte, 1500)

	n, err := upf.Read(buf)
	if err != nil {
		t.Fatalf("the heartbeat request was not answered: %v", err)
	}

	msg, err := message.Parse(buf[:n])
	if err != nil {
		t.Fatalf("parsing the answer: %v", err)
	}

	if msg.MessageType() != message.MsgTypeHeartbeatResponse || msg.Sequence() != 9 {
		t.Errorf("answer is %s seq %d, want a Heartbeat Response seq 9", msg.MessageTypeName(), msg.Sequence())
	}
}
