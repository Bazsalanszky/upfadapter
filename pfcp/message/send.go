// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
package message

import (
	"fmt"
	"net"

	"github.com/omec-project/upfadapter/logger"
	"github.com/omec-project/upfadapter/pfcp/handler"
	"github.com/omec-project/upfadapter/pfcp/udp"
	"github.com/omec-project/upfadapter/types"
	"github.com/wmnsk/go-pfcp/message"
)

func SendPfcpAssociationSetupRequest(upNodeID types.NodeID, pMsg message.Message) error {
	logger.PfcpLog.Debugf("send pfcp association request to upfNodeId [%v], pfcpMsg [%v]", upNodeID, pMsg)
	addr, err := upfAddr(upNodeID)
	if err != nil {
		return err
	}
	eventData := udp.PfcpEventData{LSEID: 0, ErrHandler: handler.HandlePfcpSendError}
	logger.PfcpLog.Debugf("send pfcp msg addr [%v], pfcpMsg [%v]", addr, pMsg)
	if err := udp.SendPfcp(pMsg, addr, eventData); err != nil {
		return err
	}
	return nil
}

func SendHeartbeatRequest(upNodeID types.NodeID, pMsg message.Message) error {
	addr, err := upfAddr(upNodeID)
	if err != nil {
		return err
	}
	err = udp.SendPfcp(pMsg, addr, nil)
	if err != nil {
		logger.PfcpLog.Errorf("failed to send heartbeat request for upf [%v]", upNodeID)
		return err
	}
	return nil
}

func SendPfcpSessionEstablishmentRequest(upNodeID types.NodeID, pMsg message.Message) error {
	upaddr, err := upfAddr(upNodeID)
	if err != nil {
		return err
	}
	eventData := udp.PfcpEventData{LSEID: 0, ErrHandler: handler.HandlePfcpSendError}
	if err := udp.SendPfcp(pMsg, upaddr, eventData); err != nil {
		return err
	}
	return nil
}

func SendPfcpSessionModificationRequest(upNodeID types.NodeID, pMsg message.Message) error {
	upaddr, err := upfAddr(upNodeID)
	if err != nil {
		return err
	}
	eventData := udp.PfcpEventData{LSEID: 0, ErrHandler: handler.HandlePfcpSendError}
	if err := udp.SendPfcp(pMsg, upaddr, eventData); err != nil {
		return err
	}
	return nil
}

func SendPfcpSessionDeletionRequest(upNodeID types.NodeID, pMsg message.Message) error {
	upaddr, err := upfAddr(upNodeID)
	if err != nil {
		return err
	}
	eventData := udp.PfcpEventData{LSEID: 0, ErrHandler: handler.HandlePfcpSendError}
	if err := udp.SendPfcp(pMsg, upaddr, eventData); err != nil {
		return err
	}
	return nil
}

// upfAddr is where PFCP for a user plane goes. A name that does not resolve is an error: it
// used to resolve to 0.0.0.0, and the request went to this host.
func upfAddr(upNodeID types.NodeID) (*net.UDPAddr, error) {
	ip, err := upNodeID.ResolveIP()
	if err != nil {
		return nil, fmt.Errorf("resolving user plane [%s]: %w", upNodeID.ValueString(), err)
	}
	return &net.UDPAddr{IP: ip, Port: udp.PFCP_PORT}, nil
}
