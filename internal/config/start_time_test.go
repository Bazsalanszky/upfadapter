// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"
	"time"

	"github.com/wmnsk/go-pfcp/ie"
)

// The adapter's Recovery Time Stamp is the time it started. Unset, it went out as the zero time,
// which reads back as 2056-03-04 17:19:00 on the wire.
func TestTheRecoveryTimeStampIsTheAdaptersStartTime(t *testing.T) {
	if UpfServerStartTime.IsZero() || UpfServerStartTime.After(time.Now()) {
		t.Fatalf("UpfServerStartTime = %v, want the time the process started", UpfServerStartTime)
	}

	onTheWire, err := ie.NewRecoveryTimeStamp(UpfServerStartTime).RecoveryTimeStamp()
	if err != nil {
		t.Fatalf("decoding the Recovery Time Stamp: %v", err)
	}

	// The IE has a resolution of one second.
	if d := UpfServerStartTime.Sub(onTheWire); d < 0 || d >= time.Second {
		t.Errorf("Recovery Time Stamp on the wire is %v, want %v to the second", onTheWire.UTC(), UpfServerStartTime.UTC())
	}
}
