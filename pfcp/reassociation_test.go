// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package pfcp

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omec-project/upfadapter/config"
	"github.com/omec-project/upfadapter/pfcp/udp"
	"github.com/omec-project/upfadapter/types"
	"github.com/wmnsk/go-pfcp/ie"
	pfcp_message "github.com/wmnsk/go-pfcp/message"
)

// The Recovery Time Stamps the tests use. A UPF that drops an association and accepts a new one
// answers under a new value: pfcpiface takes it from the peer connection, which it creates again
// after its read timeout. A restarted UPF does the same.
var (
	setUpUnder = time.Unix(1_790_000_000, 0)
	answersNow = time.Unix(1_790_000_120, 0)
)

func n4Socket(t *testing.T, addr string) *net.UDPConn {
	t.Helper()

	packet, err := (&net.ListenConfig{}).ListenPacket(context.Background(), "udp", addr)
	if err != nil {
		t.Fatalf("listen on %s: %v", addr, err)
	}

	conn, ok := packet.(*net.UDPConn)
	if !ok {
		t.Fatalf("listener is %T, want *net.UDPConn", packet)
	}

	return conn
}

// fakeUpf answers on <ip>:8805 the way a user plane does: a Heartbeat Request and an Association
// Setup Request under its current Recovery Time Stamp. It records every request it gets.
type fakeUpf struct {
	conn     *net.UDPConn
	done     chan struct{}
	ip       string
	requests []pfcp_message.Message
	nodeID   types.NodeID
	ts       atomic.Int64
	mu       sync.Mutex
}

func startFakeUpf(t *testing.T, ip string, ts time.Time) *fakeUpf {
	t.Helper()

	upf := &fakeUpf{
		conn:   n4Socket(t, net.JoinHostPort(ip, "8805")),
		ip:     ip,
		nodeID: *types.NewNodeID(ip),
		done:   make(chan struct{}),
	}
	upf.ts.Store(ts.Unix())

	go upf.serve()

	return upf
}

func (u *fakeUpf) serve() {
	defer close(u.done)

	buf := make([]byte, 2048)

	for {
		n, from, err := u.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}

		req, err := pfcp_message.Parse(buf[:n])
		if err != nil {
			continue
		}

		u.mu.Lock()
		u.requests = append(u.requests, req)
		u.mu.Unlock()

		ts := ie.NewRecoveryTimeStamp(time.Unix(u.ts.Load(), 0))

		var rsp pfcp_message.Message

		switch req.MessageType() {
		case pfcp_message.MsgTypeHeartbeatRequest:
			rsp = pfcp_message.NewHeartbeatResponse(req.Sequence(), ts)
		case pfcp_message.MsgTypeAssociationSetupRequest:
			rsp = pfcp_message.NewAssociationSetupResponse(req.Sequence(),
				ie.NewCause(ie.CauseRequestAccepted), ie.NewNodeID(u.ip, "", ""), ts)
		default:
			continue
		}

		out := make([]byte, rsp.MarshalLen())
		if err := rsp.MarshalTo(out); err != nil {
			continue
		}

		if _, err := u.conn.WriteToUDP(out, from); err != nil {
			return
		}
	}
}

// received returns the types of the requests the UPF got, in order, and their sequence numbers.
func (u *fakeUpf) received() ([]uint8, []uint32) {
	u.mu.Lock()
	defer u.mu.Unlock()

	kinds := make([]uint8, 0, len(u.requests))
	seqs := make([]uint32, 0, len(u.requests))

	for _, req := range u.requests {
		kinds = append(kinds, req.MessageType())
		seqs = append(seqs, req.Sequence())
	}

	return kinds, seqs
}

func (u *fakeUpf) got(msgType uint8) int {
	kinds, _ := u.received()

	count := 0

	for _, got := range kinds {
		if got == msgType {
			count++
		}
	}

	return count
}

// startAdapter gives the adapter a socket and reads it as udp.Run does: a response ends the
// transaction it answers and is dispatched. The UPF's answers reach the adapter through the
// dispatcher, as in production.
//
// The cleanup waits for the transactions the test started to end before it puts udp.Server back,
// so no transaction goroutine reads it while it changes.
func startAdapter(t *testing.T, upf *fakeUpf, smfSeqs ...uint32) {
	t.Helper()

	conn := n4Socket(t, "127.0.0.1:0")
	server := &udp.PfcpServer{Addr: conn.LocalAddr().(*net.UDPAddr), Conn: conn}

	previous := udp.Server
	udp.Server = server

	loopDone := make(chan struct{})

	go func() {
		defer close(loopDone)

		buf := make([]byte, 2048)

		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}

			msg, err := pfcp_message.Parse(buf[:n])
			if err != nil {
				continue
			}

			if requests, ok := server.ConsumerTable.Load(server.Addr.String()); ok {
				if tx, ok := requests.Load(msg.Sequence()); ok {
					select {
					case tx.EventChannel <- udp.ReceiveValidResponse:
					default:
					}
				}
			}

			Dispatch(msg, from)
		}
	}()

	t.Cleanup(func() {
		_, upfSeqs := upf.received()

		deadline := time.Now().Add(2 * time.Second)

		for _, seq := range append(upfSeqs, smfSeqs...) {
			for {
				requests, ok := server.ConsumerTable.Load(server.Addr.String())
				if !ok {
					break
				}

				if _, held := requests.Load(seq); !held {
					break
				}

				if time.Now().After(deadline) {
					t.Errorf("the transaction for seq[%d] did not end", seq)
					break
				}

				time.Sleep(10 * time.Millisecond)
			}
		}

		for _, c := range []*net.UDPConn{upf.conn, conn} {
			if err := c.Close(); err != nil {
				t.Logf("closing: %v", err)
			}
		}

		<-upf.done
		<-loopDone

		udp.Server = previous
	})
}

// associateAdapter puts the adapter in the state an earlier SMF left it in: associated with upf,
// holding the UPF's answer, set up under ts.
func associateAdapter(t *testing.T, fake *fakeUpf, ts time.Time) {
	t.Helper()

	upf := fake.nodeID

	config.InsertUpfNode(upf)

	node := config.GetUpfFromNodeId(&upf)
	if node == nil {
		t.Fatal("the user plane was not recorded")
	}

	t.Cleanup(func() {
		config.UpfCfg.UpfListLock.Lock()
		delete(config.UpfCfg.UPFs, string(upf.NodeIdValue))
		config.UpfCfg.UpfListLock.Unlock()
	})

	node.PreservePfcpAssociationRsp(*pfcp_message.NewAssociationSetupResponse(1,
		ie.NewCause(ie.CauseRequestAccepted), ie.NewNodeID(fake.ip, "", ""), ie.NewRecoveryTimeStamp(ts)))

	config.UpfCfg.UpfListLock.Lock()
	node.State = config.AssociatedSetUpSuccess
	config.UpfCfg.UpfListLock.Unlock()
}

// heldUnder is the Recovery Time Stamp of the UPF answer the adapter holds.
func heldUnder(t *testing.T, upf types.NodeID) time.Time {
	t.Helper()

	node := config.GetUpfFromNodeId(&upf)
	if node == nil {
		t.Fatal("the user plane is not recorded")
	}

	node.UpfLock.RLock()
	stored := node.LastAssoRsp
	node.UpfLock.RUnlock()

	return recoveryOf(t, &stored)
}

func smfAssociationSetupRequest(seq uint32) *pfcp_message.AssociationSetupRequest {
	return pfcp_message.NewAssociationSetupRequest(seq,
		ie.NewNodeID("10.42.0.1", "", ""), ie.NewRecoveryTimeStamp(time.Now()))
}

func recoveryOf(t *testing.T, rsp *pfcp_message.AssociationSetupResponse) time.Time {
	t.Helper()

	if rsp.RecoveryTimeStamp == nil {
		t.Fatal("the answer has no recovery timestamp")
	}

	ts, err := rsp.RecoveryTimeStamp.RecoveryTimeStamp()
	if err != nil {
		t.Fatalf("reading the answer's recovery timestamp: %v", err)
	}

	return ts
}

// The lab bug (cause 72 after an SMF restart). The adapter was associated with the UPF for an
// earlier SMF. That SMF went away, and pfcpiface dropped the association once it had read nothing
// from the adapter for 15 s. A new SMF then asks to associate. The adapter used to answer it from
// its stored copy of the UPF's first answer: the request never reached the UPF, the SMF believed
// itself associated, and the UPF refused every session with "No established PFCP Association".
func TestAnAssociationTheUPFNoLongerHoldsIsSetUpAgain(t *testing.T) {
	upf := startFakeUpf(t, "127.0.0.41", answersNow)
	startAdapter(t, upf, 21)
	associateAdapter(t, upf, setUpUnder)

	b, err := ForwardPfcpMsgToUpf(smfAssociationSetupRequest(21), upf.nodeID)
	if err != nil {
		t.Fatalf("forwarding the SMF's association setup request: %v", err)
	}

	if got := upf.got(pfcp_message.MsgTypeAssociationSetupRequest); got != 1 {
		t.Errorf("the UPF got %d Association Setup Requests, want 1: the SMF was answered for an association the UPF no longer holds", got)
	}

	rsp, err := pfcp_message.ParseAssociationSetupResponse(b)
	if err != nil {
		t.Fatalf("parsing the answer: %v", err)
	}

	if rsp.Sequence() != 21 {
		t.Errorf("answered under seq %d, want the SMF's 21", rsp.Sequence())
	}

	if got := recoveryOf(t, rsp); !got.Equal(answersNow) {
		t.Errorf("the SMF was answered under recovery timestamp %v, want the UPF's current %v", got, answersNow)
	}

	if !config.IsUpfAssociated(upf.nodeID) {
		t.Error("the adapter does not count itself associated after the UPF accepted")
	}

	if held := heldUnder(t, upf.nodeID); !held.Equal(answersNow) {
		t.Errorf("the adapter holds the UPF's answer under %v, want the new one under %v", held, answersNow)
	}
}

// The other half of the same run: the new SMF asked while the UPF still held the association
// (pfcpiface timed it out 1.1 s later). Answering from the stored copy is right then, but sending
// the UPF nothing is not: a UPF that ages associations out needs to hear from the adapter, or it
// drops the association before the new SMF's own heartbeats start.
func TestAnAssociationTheUPFStillHoldsIsAnsweredFromTheStoredCopy(t *testing.T) {
	upf := startFakeUpf(t, "127.0.0.42", setUpUnder)
	startAdapter(t, upf, 22)
	associateAdapter(t, upf, setUpUnder)

	b, err := ForwardPfcpMsgToUpf(smfAssociationSetupRequest(22), upf.nodeID)
	if err != nil {
		t.Fatalf("answering the SMF's association setup request: %v", err)
	}

	if got := upf.got(pfcp_message.MsgTypeHeartbeatRequest); got != 1 {
		t.Errorf("the UPF got %d Heartbeat Requests, want 1: the adapter answered for an association it had not checked", got)
	}

	if got := upf.got(pfcp_message.MsgTypeAssociationSetupRequest); got != 0 {
		t.Errorf("the UPF got %d Association Setup Requests, want 0: an association it still holds was replaced", got)
	}

	rsp, err := pfcp_message.ParseAssociationSetupResponse(b)
	if err != nil {
		t.Fatalf("parsing the answer: %v", err)
	}

	if rsp.Sequence() != 22 {
		t.Errorf("answered under seq %d, want the SMF's 22", rsp.Sequence())
	}

	if got := recoveryOf(t, rsp); !got.Equal(setUpUnder) {
		t.Errorf("the SMF was answered under recovery timestamp %v, want the stored %v", got, setUpUnder)
	}

	if !config.IsUpfAssociated(upf.nodeID) {
		t.Error("the adapter no longer counts itself associated")
	}
}

// The SMF's heartbeats are relayed. When the UPF answers one under a new Recovery Time Stamp, it
// restarted or dropped the association, and the adapter's association is gone. The SMF sees the
// change too and asks to associate again; that request has to reach the UPF.
func TestARelayedHeartbeatUnderANewRecoveryTimeStampEndsTheAssociation(t *testing.T) {
	for _, tc := range []struct {
		answers        time.Time
		name           string
		wantAssociated bool
	}{
		{name: "new recovery timestamp", answers: answersNow, wantAssociated: false},
		{name: "same recovery timestamp", answers: setUpUnder, wantAssociated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upf := startFakeUpf(t, "127.0.0.43", tc.answers)
			startAdapter(t, upf, 23)
			associateAdapter(t, upf, setUpUnder)

			heartbeat := pfcp_message.NewHeartbeatRequest(23, ie.NewRecoveryTimeStamp(time.Now()), nil)

			b, err := ForwardPfcpMsgToUpf(heartbeat, upf.nodeID)
			if err != nil {
				t.Fatalf("relaying the SMF's heartbeat: %v", err)
			}

			rsp, err := pfcp_message.ParseHeartbeatResponse(b)
			if err != nil {
				t.Fatalf("parsing the relayed answer: %v", err)
			}

			if ts, err := rsp.RecoveryTimeStamp.RecoveryTimeStamp(); err != nil || !ts.Equal(tc.answers) || rsp.Sequence() != 23 {
				t.Errorf("relayed seq %d under %v (%v), want the UPF's answer unchanged: seq 23 under %v",
					rsp.Sequence(), ts, err, tc.answers)
			}

			if got := config.IsUpfAssociated(upf.nodeID); got != tc.wantAssociated {
				t.Errorf("associated after the heartbeat: %v, want %v", got, tc.wantAssociated)
			}
		})
	}
}
