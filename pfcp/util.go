// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
package pfcp

import (
	"fmt"
	"net"

	"github.com/omec-project/upfadapter/config"
	"github.com/omec-project/upfadapter/logger"
	"github.com/omec-project/upfadapter/pfcp/handler"
	"github.com/omec-project/upfadapter/pfcp/message"
	"github.com/omec-project/upfadapter/types"
	"github.com/wmnsk/go-pfcp/ie"
	pfcp_message "github.com/wmnsk/go-pfcp/message"
)

func ForwardPfcpMsgToUpf(pfcpMessage pfcp_message.Message, upNodeID types.NodeID) ([]byte, error) {
	// Room for one answer. The answer is sent from the PFCP receive path, which must not block on
	// a requester that is no longer waiting -- a send that failed leaves this HTTP handler on its
	// way out, and an unbuffered channel would wedge the receive goroutine on it for good.
	pfcpTxnChan := make(config.PfcpTxnChan, 1)
	var err error

	// identify msg type
	msgType := pfcpMessage.MessageType()
	switch msgType {
	case pfcp_message.MsgTypeAssociationSetupRequest:
		// if UPF is already associated then send Asso rsp
		if config.IsUpfAssociated(upNodeID) {
			logger.AppLog.Debugf("upf[%v] already associated", upNodeID)
			// form and send Assoc rsp
			pfcpAssociationRsp, err1 := handler.BuildPfcpAssociationResponse(&upNodeID, pfcpMessage.Sequence())
			if err1 != nil {
				return nil, fmt.Errorf("error building association response: %v", err1)
			}
			answerRetention(pfcpAssociationRsp, pfcpMessage)
			buf := make([]byte, pfcpAssociationRsp.MarshalLen())
			err = pfcpAssociationRsp.MarshalTo(buf)
			if err != nil {
				return nil, err
			}
			return buf, nil
		}

		config.InsertUpfNode(upNodeID)

		associationReq, ok := pfcpMessage.(*pfcp_message.AssociationSetupRequest)
		if !ok {
			return nil, fmt.Errorf("invalid association setup request")
		}

		// replace smf time-stamp with UPF-Adapters timestamp
		associationReq.RecoveryTimeStamp = ie.NewRecoveryTimeStamp(config.UpfServerStartTime)

		// replace SMF NodeId with of upf-adapter's one
		associationReq.NodeID = adapterNodeID()
		logger.AppLog.Debugf("association request, existing node id replaced by [%v]", config.AdapterN4.NodeID.ValueString())

		// store txn in seq:chan map
		config.InsertUpfPfcpTxn(associationReq.Sequence(), pfcpTxnChan)

		if err = message.SendPfcpAssociationSetupRequest(upNodeID, associationReq); err != nil {
			// The registration goes with the send. Left behind, the next response to carry this
			// sequence number is handed to a requester that has already gone, and the answer the
			// requester it belongs to is waiting for never arrives.
			config.ForgetUpfPfcpTxn(associationReq.Sequence(), pfcpTxnChan)

			return nil, err
		}
	case pfcp_message.MsgTypeHeartbeatRequest:

		heartbeatReq, ok := pfcpMessage.(*pfcp_message.HeartbeatRequest)
		if !ok {
			return nil, fmt.Errorf("invalid heartbeat request")
		}

		// replace smf time-stamp with UPF-Adapters timestamp
		heartbeatReq.RecoveryTimeStamp = ie.NewRecoveryTimeStamp(config.UpfServerStartTime)

		// store txn in seq:chan map
		config.InsertUpfPfcpTxn(heartbeatReq.Sequence(), pfcpTxnChan)

		if err = message.SendHeartbeatRequest(upNodeID, heartbeatReq); err != nil {
			// The registration goes with the send. Left behind, the next response to carry this
			// sequence number is handed to a requester that has already gone, and the answer the
			// requester it belongs to is waiting for never arrives.
			config.ForgetUpfPfcpTxn(heartbeatReq.Sequence(), pfcpTxnChan)

			return nil, err
		}
	case pfcp_message.MsgTypeSessionEstablishmentRequest:
		sessionEstablishmentReq, ok := pfcpMessage.(*pfcp_message.SessionEstablishmentRequest)
		if !ok {
			return nil, fmt.Errorf("invalid session establishment request")
		}
		// replace SMF NodeId with of upf-adapter's one
		sessionEstablishmentReq.NodeID = adapterNodeID()

		// Replace the SMF's F-SEID addresses with the adapter's, in the slot of each one's
		// family, so the UPF sends its session reports here.
		existingCPFSEID, err1 := sessionEstablishmentReq.CPFSEID.FSEID()
		if err1 != nil {
			return nil, fmt.Errorf("error getting FSEID from session establishment request")
		}
		sessionEstablishmentReq.CPFSEID = adapterFSEID(existingCPFSEID.SEID)
		// store txn in seq:chan map
		config.InsertUpfPfcpTxn(sessionEstablishmentReq.Sequence(), pfcpTxnChan)

		if err = message.SendPfcpSessionEstablishmentRequest(upNodeID, sessionEstablishmentReq); err != nil {
			// The registration goes with the send. Left behind, the next response to carry this
			// sequence number is handed to a requester that has already gone, and the answer the
			// requester it belongs to is waiting for never arrives.
			config.ForgetUpfPfcpTxn(sessionEstablishmentReq.Sequence(), pfcpTxnChan)

			return nil, err
		}
	case pfcp_message.MsgTypeSessionModificationRequest:
		sessionModificationReq, ok := pfcpMessage.(*pfcp_message.SessionModificationRequest)
		if !ok {
			return nil, fmt.Errorf("invalid session modification request")
		}

		// store txn in seq:chan map
		config.InsertUpfPfcpTxn(sessionModificationReq.Sequence(), pfcpTxnChan)

		if err = message.SendPfcpSessionModificationRequest(upNodeID, sessionModificationReq); err != nil {
			// The registration goes with the send. Left behind, the next response to carry this
			// sequence number is handed to a requester that has already gone, and the answer the
			// requester it belongs to is waiting for never arrives.
			config.ForgetUpfPfcpTxn(sessionModificationReq.Sequence(), pfcpTxnChan)

			return nil, err
		}
	case pfcp_message.MsgTypeSessionDeletionRequest:
		sessionDeletionReq, ok := pfcpMessage.(*pfcp_message.SessionDeletionRequest)
		if !ok {
			return nil, fmt.Errorf("invalid session deletion request")
		}

		// store txn in seq:chan map
		config.InsertUpfPfcpTxn(sessionDeletionReq.Sequence(), pfcpTxnChan)

		if err = message.SendPfcpSessionDeletionRequest(upNodeID, sessionDeletionReq); err != nil {
			// The registration goes with the send. Left behind, the next response to carry this
			// sequence number is handed to a requester that has already gone, and the answer the
			// requester it belongs to is waiting for never arrives.
			config.ForgetUpfPfcpTxn(sessionDeletionReq.Sequence(), pfcpTxnChan)

			return nil, err
		}
	default:
		return nil, fmt.Errorf("invalid msg type [%v] from smf", msgType)
	}

	// wait for response from UPF
	pfcpRsp := <-pfcpTxnChan

	return pfcpRsp.Rsp, pfcpRsp.Err
}

// psrei is the PSREI (PFCP Session Retained Indication) bit of PFCPASRsp-Flags, TS 29.244 clause
// 8.2.134 (bit 1 of octet 5).
const psrei = 0x01

// answerRetention sets PSREI on an association response this adapter gives from its stored copy of
// the UPF's, as the request asked.
//
// TS 29.244 clause 6.2.6.2.2: a request for a node ID that already has an association replaces it,
// and the UP function deletes that association's sessions unless the request carries PFCP Session
// Retention Information; if it does and they were retained, the response sets PSREI. Answered here,
// the request never reaches the UPF and the adapter's own association with it is not replaced, so
// the UPF keeps the sessions for as long as that association stands, and PSREI is the answer to
// whether the SMF asked. The adapter does not detect a UPF restart or the loss of its association,
// and once associated it answers from the stored copy regardless, so in that state this says the
// sessions were retained when the UPF no longer has them. The stored copy is the UPF's answer to
// an earlier request, so its PSREI answered that one and is not passed on; its other flags are the
// UPF's and are kept.
func answerRetention(rsp *pfcp_message.AssociationSetupResponse, req pfcp_message.Message) {
	var flags uint8
	if rsp.PFCPASRspFlags != nil {
		// A flags IE the stored answer cannot be read from carries nothing to keep.
		if stored, err := rsp.PFCPASRspFlags.PFCPASRspFlags(); err == nil {
			flags = stored
		}
	}
	flags &^= psrei
	if asreq, ok := req.(*pfcp_message.AssociationSetupRequest); ok && asreq.PFCPSessionRetentionInformation != nil {
		flags |= psrei
	}

	rsp.PFCPASRspFlags = nil
	if flags != 0 {
		rsp.PFCPASRspFlags = ie.NewPFCPASRspFlags(flags)
	}
}

// adapterNodeID is the Node ID IE the adapter puts in place of the SMF's: its own Node ID, of
// whichever type it is. It used to be built as IPv4 from UpfAdapterIp, so an adapter with an
// IPv6 address sent 0.0.0.0. An adapter with no address at all still sends the IPv4 Node ID
// 0.0.0.0 it always did.
func adapterNodeID() *ie.IE {
	nodeID := config.AdapterN4.NodeID
	if len(nodeID.NodeIdValue) == 0 {
		return ie.NewNodeID(net.IP(nil).String(), "", "")
	}
	return ie.NewNodeIDHeuristic(nodeID.ValueString())
}

// adapterFSEID is the CP F-SEID the adapter puts in place of the SMF's: the SMF's SEID with
// the adapter's address in each family it has one in (TS 29.244 clause 8.2.37 allows both).
// It used to carry UpfAdapterIp in the IPv4 slot only, which was nil on an IPv6 pod, so the
// F-SEID named no address and the UPF had nowhere to send its reports.
func adapterFSEID(seid uint64) *ie.IE {
	return ie.NewFSEID(seid, config.AdapterN4.IPv4, config.AdapterN4.IPv6)
}
