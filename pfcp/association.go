// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package pfcp

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/omec-project/upfadapter/config"
	"github.com/omec-project/upfadapter/logger"
	"github.com/omec-project/upfadapter/pfcp/message"
	"github.com/omec-project/upfadapter/pfcp/udp"
	"github.com/omec-project/upfadapter/types"
	"github.com/wmnsk/go-pfcp/ie"
	pfcp_message "github.com/wmnsk/go-pfcp/message"
)

// associationProbeTimeout bounds the wait for the user plane's answer to a probe. It outlasts the
// transaction's own resends (udp.NumOfResend x udp.ResendRequestTimeOutPeriod), so a UPF that
// answers the last of them is still heard. The probe and the Association Setup Request it can lead
// to together stay inside the SMF's 30 s wait for the adapter's answer.
var associationProbeTimeout = time.Duration(udp.NumOfResend*udp.ResendRequestTimeOutPeriod)*time.Second + time.Second

const (
	// The adapter numbers its probes down from the top of the sequence space. The SMF counts up
	// from zero and the session report relay counts up from the halfway point, so the three rarely
	// meet; when they do, the probe is not registered over the other request (ClaimUpfPfcpTxn,
	// udp.ErrDuplicateSequence) and takes the next number.
	probeSequenceTop  = 0xFFFFFF
	probeSequenceSpan = 0x10000
	probeSendAttempts = 4
)

var probeSequence atomic.Uint32

func nextProbeSequence() uint32 {
	return probeSequenceTop - (probeSequence.Add(1)-1)%probeSequenceSpan
}

// associationStillHeld reports whether the user plane still holds the association the adapter
// holds with it, by asking: it sends a Heartbeat Request of the adapter's own and compares the
// Recovery Time Stamp in the answer with the one the association was set up under.
//
// The adapter answers an SMF's Association Setup Request from its stored copy of the user plane's
// answer while it is associated, so that a second SMF, or a restarted one, does not replace the
// association the user plane's sessions hang on. That is only right while the user plane still
// holds it. pfcpiface drops a peer's association, and its sessions, once it has read nothing from
// that peer for its read timeout (15 s by default), and says nothing to the peer when it does. An
// SMF that restarts takes its heartbeats with it, so by the time the new SMF asks, the stored copy
// can describe an association that is gone; every session the SMF then sets up is refused with
// Cause 72. A UPF that restarted is in the same state.
//
// The probe also counts as traffic to a UPF that ages associations out, so an association that is
// still held is kept until the new SMF's own heartbeats take over.
//
// When the answer does not confirm the association -- a different Recovery Time Stamp, or none in
// time -- the association is recorded as ended, and the caller sends the SMF's request on.
func associationStillHeld(upNodeID types.NodeID) bool {
	held, ok := config.AssociationRecoveryTimeStamp(upNodeID)
	if !ok {
		return false
	}

	answered, err := probeRecoveryTimeStamp(upNodeID)
	if err != nil {
		if config.EndAssociation(upNodeID, held) {
			logger.PfcpLog.Warnf("upf [%s] did not confirm its association with the adapter (%v); setting it up again",
				nodeName(upNodeID), err)
		}

		return false
	}

	if !answered.Equal(held) {
		if config.EndAssociation(upNodeID, held) {
			logger.PfcpLog.Warnf("upf [%s] answers under recovery timestamp [%v], its association with the adapter was set up under [%v]: the association is gone; setting it up again",
				nodeName(upNodeID), answered, held)
		}

		return false
	}

	logger.PfcpLog.Debugf("upf [%s] still holds its association with the adapter", nodeName(upNodeID))

	return true
}

// probeRecoveryTimeStamp sends the user plane a Heartbeat Request numbered by the adapter and
// returns the Recovery Time Stamp it answers with.
func probeRecoveryTimeStamp(upNodeID types.NodeID) (time.Time, error) {
	for range probeSendAttempts {
		seq := nextProbeSequence()
		answer := make(config.PfcpTxnChan, 1)

		if !config.ClaimUpfPfcpTxn(seq, answer) {
			continue
		}

		request := pfcp_message.NewHeartbeatRequest(seq, ie.NewRecoveryTimeStamp(config.UpfServerStartTime), nil)

		err := message.SendHeartbeatRequest(upNodeID, request)
		if errors.Is(err, udp.ErrDuplicateSequence) {
			config.ForgetUpfPfcpTxn(seq, answer)

			continue
		}

		if err != nil {
			config.ForgetUpfPfcpTxn(seq, answer)

			return time.Time{}, err
		}

		timeout := time.NewTimer(associationProbeTimeout)

		select {
		case rsp := <-answer:
			timeout.Stop()

			if rsp.Err != nil {
				return time.Time{}, rsp.Err
			}

			return heartbeatRecoveryTimeStamp(rsp.Rsp)
		case <-timeout.C:
			config.ForgetUpfPfcpTxn(seq, answer)

			return time.Time{}, fmt.Errorf("no heartbeat response in %v", associationProbeTimeout)
		}
	}

	return time.Time{}, fmt.Errorf("no free sequence number for a heartbeat request in %d attempts", probeSendAttempts)
}

// noteHeartbeatRecovery checks the answer to a heartbeat the SMF sent through the adapter. A
// Recovery Time Stamp other than the association's means the user plane restarted, or dropped the
// association and is answering on a new one, and so the association the adapter holds is gone.
// The SMF sees the same change in the relayed answer and sets up its association again; recording
// the end here is what sends that request to the user plane.
func noteHeartbeatRecovery(upNodeID types.NodeID, rsp []byte) {
	held, ok := config.AssociationRecoveryTimeStamp(upNodeID)
	if !ok {
		return
	}

	answered, err := heartbeatRecoveryTimeStamp(rsp)
	if err != nil {
		logger.PfcpLog.Debugf("relayed heartbeat response from upf [%s]: %v", nodeName(upNodeID), err)
		return
	}

	if !answered.Equal(held) && config.EndAssociation(upNodeID, held) {
		logger.PfcpLog.Warnf("upf [%s] answers heartbeats under recovery timestamp [%v], its association with the adapter was set up under [%v]: the association is gone",
			nodeName(upNodeID), answered, held)
	}
}

func heartbeatRecoveryTimeStamp(b []byte) (time.Time, error) {
	rsp, err := pfcp_message.ParseHeartbeatResponse(b)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing the heartbeat response: %w", err)
	}

	if rsp.RecoveryTimeStamp == nil {
		return time.Time{}, errors.New("the heartbeat response has no recovery timestamp")
	}

	return rsp.RecoveryTimeStamp.RecoveryTimeStamp()
}

func nodeName(id types.NodeID) string {
	if id.NodeIdType == types.NodeIdTypeFqdn {
		return string(id.NodeIdValue)
	}

	return net.IP(id.NodeIdValue).String()
}
