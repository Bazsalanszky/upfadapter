// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package pfcp

import (
	"net"
	"testing"

	"github.com/omec-project/upfadapter/config"
	"github.com/omec-project/upfadapter/types"
	"github.com/wmnsk/go-pfcp/ie"
)

func withAdapterN4(t *testing.T, local config.LocalN4) {
	t.Helper()
	prev := config.AdapterN4
	config.AdapterN4 = local
	t.Cleanup(func() { config.AdapterN4 = prev })
}

// The Node ID and the F-SEID the adapter substitutes for the SMF's are its own, by family. They
// were built as IPv4 from POD_IP.To4(): on an IPv6 pod the Node ID was 0.0.0.0 and the F-SEID
// named no address, so the UPF had nowhere to send reports.
func TestAdapterIdentityByFamily(t *testing.T) {
	v4, v6 := net.ParseIP("10.42.0.50").To4(), net.ParseIP("2001:db8:600::50")
	for _, tc := range []struct {
		name           string
		wantNodeID     string
		wantV4, wantV6 net.IP
		local          config.LocalN4
		wantType       uint8
	}{
		{"IPv4", v4.String(), v4, nil, config.LocalN4{NodeID: *types.NewNodeID(v4.String()), IPv4: v4}, ie.NodeIDIPv4Address},
		{"IPv6", v6.String(), nil, v6, config.LocalN4{NodeID: *types.NewNodeID(v6.String()), IPv6: v6}, ie.NodeIDIPv6Address},
		{"dual-stack", v6.String(), v4, v6, config.LocalN4{NodeID: *types.NewNodeID(v6.String()), IPv4: v4, IPv6: v6}, ie.NodeIDIPv6Address},
		{"FQDN Node ID", "upf-adapter.example", nil, v6, config.LocalN4{NodeID: *types.NewNodeID("upf-adapter.example"), IPv6: v6}, ie.NodeIDFQDN},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withAdapterN4(t, tc.local)

			nodeID := adapterNodeID()
			if got, err := nodeID.NodeID(); err != nil || got != tc.wantNodeID || nodeID.Payload[0] != tc.wantType {
				t.Errorf("Node ID = %q (type %d), %v; want %q (type %d)", got, nodeID.Payload[0], err, tc.wantNodeID, tc.wantType)
			}

			f, err := adapterFSEID(0x99).FSEID()
			if err != nil {
				t.Fatal(err)
			}
			if f.SEID != 0x99 {
				t.Errorf("SEID = %#x, want 0x99", f.SEID)
			}
			if f.HasIPv4() != (tc.wantV4 != nil) || (tc.wantV4 != nil && !f.IPv4Address.Equal(tc.wantV4)) {
				t.Errorf("F-SEID IPv4 = %t %v, want %v", f.HasIPv4(), f.IPv4Address, tc.wantV4)
			}
			if f.HasIPv6() != (tc.wantV6 != nil) || (tc.wantV6 != nil && !f.IPv6Address.Equal(tc.wantV6)) {
				t.Errorf("F-SEID IPv6 = %t %v, want %v", f.HasIPv6(), f.IPv6Address, tc.wantV6)
			}
		})
	}
}

// An adapter with no address sends the IPv4 Node ID 0.0.0.0, as it did.
func TestAdapterNodeIDWithoutAnAddress(t *testing.T) {
	withAdapterN4(t, config.LocalN4{NodeID: types.NodeID{NodeIdType: types.NodeIdTypeIpv4Address}})
	got, want := adapterNodeID(), ie.NewNodeID(net.IP(nil).String(), "", "")
	if string(got.Payload) != string(want.Payload) {
		t.Errorf("Node ID payload = %x, want %x", got.Payload, want.Payload)
	}
}
