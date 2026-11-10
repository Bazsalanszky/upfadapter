// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"testing"
	"time"

	"github.com/omec-project/upfadapter/config"
	"github.com/omec-project/upfadapter/types"
	"github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"
)

// The Association Setup Response of a UPF that identifies itself by an IPv6 address activates
// that UPF and is handed to the waiting request. The UPF used to go unfound -- only IPv4 Node
// IDs were compared -- and the handler then dereferenced the nil UPF, which, on the dispatch
// goroutine, took the adapter down.
func TestAnIPv6UPFsAssociationResponseIsHandled(t *testing.T) {
	const upfIP = "2001:db8:640::2"
	nodeID := types.NewNodeID(upfIP)
	config.InsertUpfNode(*nodeID)

	const seq = 0x640002
	answer := make(config.PfcpTxnChan, 1)
	config.InsertUpfPfcpTxn(seq, answer)
	t.Cleanup(func() { config.ForgetUpfPfcpTxn(seq, answer) })

	HandlePfcpAssociationSetupResponse(message.NewAssociationSetupResponse(seq,
		ie.NewNodeIDHeuristic(upfIP),
		ie.NewCause(ie.CauseRequestAccepted),
		ie.NewRecoveryTimeStamp(time.Now()),
	))

	select {
	case rsp := <-answer:
		if rsp.Err != nil || len(rsp.Rsp) == 0 {
			t.Errorf("the waiting request got %+v, want the response", rsp)
		}
	default:
		t.Fatal("the waiting request was not answered")
	}
	if !config.IsUpfAssociated(*nodeID) {
		t.Error("the IPv6 UPF is not associated")
	}
}
