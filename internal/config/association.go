// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"time"

	"github.com/omec-project/upfadapter/internal/logger"
	"github.com/omec-project/upfadapter/internal/types"
)

// AssociationRecoveryTimeStamp returns the user plane's Recovery Time Stamp from the association
// the adapter holds with it on behalf of the SMF that names it nodeId, and false when the adapter
// holds none. A Recovery Time Stamp that cannot be read counts as none: the adapter has nothing to
// tell its association apart from a newer one by.
//
// The value identifies the association, not only the user plane's start: a UPF that drops an
// association and accepts a new one (pfcpiface does so per peer connection) answers under a new
// Recovery Time Stamp, as a restarted one does.
func AssociationRecoveryTimeStamp(nodeId types.NodeID) (time.Time, bool) {
	UpfCfg.UpfListLock.RLock()
	upf := UpfCfg.UPFs[string(nodeId.NodeIdValue)]
	associated := upf != nil && upf.State == AssociatedSetUpSuccess
	UpfCfg.UpfListLock.RUnlock()

	if !associated {
		return time.Time{}, false
	}

	return upf.storedRecoveryTimeStamp()
}

// EndAssociation records that the user plane no longer holds the association the adapter was
// holding with it, so that the next Association Setup Request from an SMF goes to the user plane
// instead of being answered from the stored copy of its last answer.
//
// It ends the association only while that is still the one identified by held. Another request
// can set up a new association between the caller reading held and acting on it, and ending that
// one would send the next SMF's request on for nothing. It reports whether it ended one.
func EndAssociation(nodeId types.NodeID, held time.Time) bool {
	UpfCfg.UpfListLock.Lock()
	defer UpfCfg.UpfListLock.Unlock()

	upf := UpfCfg.UPFs[string(nodeId.NodeIdValue)]
	if upf == nil || upf.State != AssociatedSetUpSuccess {
		return false
	}

	// Lock order: UpfListLock, then UpfLock. Nothing takes them the other way round.
	if current, ok := upf.storedRecoveryTimeStamp(); !ok || !current.Equal(held) {
		return false
	}

	upf.State = NotAssociated
	logger.CfgLog.Infof("upf node [%v] is no longer associated", upf.UpfName)

	return true
}

func (upf *UPNode) storedRecoveryTimeStamp() (time.Time, bool) {
	upf.UpfLock.RLock()
	defer upf.UpfLock.RUnlock()

	if upf.LastAssoRsp.RecoveryTimeStamp == nil {
		return time.Time{}, false
	}

	ts, err := upf.LastAssoRsp.RecoveryTimeStamp.RecoveryTimeStamp()
	if err != nil {
		return time.Time{}, false
	}

	return ts, true
}

// ClaimUpfPfcpTxn registers a wait for the response under seq only if nothing is waiting under
// it already, and reports whether it did. InsertUpfPfcpTxn replaces what is there, which is right
// for a number the SMF chose; a number the adapter chooses for a request of its own must not take
// the place of an SMF request in flight.
func ClaimUpfPfcpTxn(seq uint32, pfcpTxnChan PfcpTxnChan) bool {
	UpfTxnsMutex.Lock()
	defer UpfTxnsMutex.Unlock()

	if _, taken := UpfTxns[seq]; taken {
		return false
	}

	UpfTxns[seq] = pfcpTxnChan

	return true
}
