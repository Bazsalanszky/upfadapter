// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package config

import "time"

// UpfServerStartTime is the adapter's Recovery Time Stamp: the time it started (TS 29.244 clause
// 8.2.65). It goes in the Association Setup and Heartbeat Requests the adapter relays and in the
// Heartbeat Responses it gives.
//
// It was declared but never set, so the adapter sent the zero time.Time, which go-pfcp encodes as
// 2056-03-04 17:19:00: the same value from every adapter and after every restart, so a UPF could
// not tell that its control-plane peer had restarted.
func init() {
	UpfServerStartTime = time.Now()
}
