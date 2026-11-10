// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"net"
	"testing"

	"github.com/omec-project/upfadapter/types"
)

func TestResolveLocalN4(t *testing.T) {
	const (
		v4 = "10.42.0.30"
		v6 = "2001:db8:600::30"
	)
	for _, tc := range []struct {
		name                     string
		podIP, podIPs, nodeID    string
		family                   string
		wantNodeID               string
		wantV4, wantV6, wantBind string // "" = none / wildcard
		wantFamily               types.AddressFamily
	}{
		{name: "IPv4 POD_IP, the default chart", podIP: v4, wantNodeID: v4, wantV4: v4, wantBind: v4},
		{name: "IPv6 POD_IP", podIP: v6, wantNodeID: v6, wantV6: v6, wantBind: v6, wantFamily: types.AddressFamilyIPv6},
		{name: "POD_IPS, IPv4 primary", podIP: v4, podIPs: v4 + "," + v6, wantNodeID: v4, wantV4: v4, wantV6: v6},
		{name: "POD_IPS, IPv6 primary", podIP: v6, podIPs: v6 + "," + v4, wantNodeID: v6, wantV4: v4, wantV6: v6},
		{name: "POD_IPS with IPv6 preferred", podIPs: v4 + "," + v6, family: "ipv6", wantNodeID: v6, wantV4: v4, wantV6: v6, wantFamily: types.AddressFamilyIPv6},
		{name: "FQDN Node ID", podIP: v6, nodeID: "upf-adapter.example", wantNodeID: "upf-adapter.example", wantV6: v6, wantBind: v6, wantFamily: types.AddressFamilyIPv6},
		{name: "no address", wantNodeID: "<nil>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local, family, err := resolveLocalN4(tc.podIP, tc.podIPs, tc.nodeID, tc.family)
			if err != nil {
				t.Fatal(err)
			}
			if got := local.NodeID.ValueString(); got != tc.wantNodeID {
				t.Errorf("Node ID = %q, want %q", got, tc.wantNodeID)
			}
			checkIP(t, "IPv4", local.IPv4, tc.wantV4)
			checkIP(t, "IPv6", local.IPv6, tc.wantV6)
			checkIP(t, "bind", local.ListenAddr(8805).IP, tc.wantBind)
			if family != tc.wantFamily {
				t.Errorf("resolve family = %v, want %v", family, tc.wantFamily)
			}
		})
	}

	if _, _, err := resolveLocalN4("not-an-ip", "", "", ""); err == nil {
		t.Error("a POD_IP that is not an IP was accepted")
	}
	if _, _, err := resolveLocalN4("10.0.0.1", "", "", "inet6"); err == nil {
		t.Error("an unknown family was accepted")
	}
}

// With an IPv4 POD_IP the adapter's address and Node ID are what they were: POD_IP.To4(), and
// an IPv4 Node ID of those four octets.
func TestResolveLocalN4KeepsIPv4(t *testing.T) {
	const podIP = "10.42.0.31"
	local, _, err := resolveLocalN4(podIP, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	legacy := net.ParseIP(podIP).To4()
	if got := local.primaryIP(); string(got) != string(legacy) {
		t.Errorf("UpfAdapterIp = %v (%d bytes), want %v (%d bytes)", got, len(got), legacy, len(legacy))
	}
	if local.NodeID.NodeIdType != types.NodeIdTypeIpv4Address || string(local.NodeID.NodeIdValue) != string(legacy) {
		t.Errorf("Node ID = %+v, want IPv4 %v", local.NodeID, legacy)
	}
}

// A UPF that identifies itself by an IPv6 address is found. Only IPv4 Node IDs were compared,
// so the association response of an IPv6 UPF found no UPF.
func TestGetUpfFromNodeIdMatchesIPv6(t *testing.T) {
	prev := UpfCfg.UPFs
	UpfCfg.UPFs = make(map[string]*UPNode)
	t.Cleanup(func() { UpfCfg.UPFs = prev })

	a, b := types.NewNodeID("2001:db8:640::a"), types.NewNodeID("2001:db8:640::b")
	InsertUpfNode(*a)
	InsertUpfNode(*b)
	InsertUpfNode(*types.NewNodeID("192.0.2.40"))

	for _, nodeID := range []*types.NodeID{a, b} {
		upf := GetUpfFromNodeId(nodeID)
		if upf == nil || !upf.ANIP.Equal(net.IP(nodeID.NodeIdValue)) {
			t.Errorf("GetUpfFromNodeId(%s) = %v", nodeID.ValueString(), upf)
		}
	}
	if upf := GetUpfFromNodeId(types.NewNodeID("192.0.2.40")); upf == nil {
		t.Error("the IPv4 UPF is no longer found")
	}
	if upf := GetUpfFromNodeId(types.NewNodeID("2001:db8:640::c")); upf != nil {
		t.Errorf("an unknown IPv6 Node ID matched %v", upf.UpfName)
	}
	if upf := ActivateUpfNode(b); upf == nil || upf.State != AssociatedSetUpSuccess {
		t.Errorf("ActivateUpfNode(%s) = %v", b.ValueString(), upf)
	}
}

func checkIP(t *testing.T, what string, got net.IP, want string) {
	t.Helper()
	if want == "" {
		if got != nil {
			t.Errorf("%s = %v, want none", what, got)
		}
		return
	}
	if !got.Equal(net.ParseIP(want)) {
		t.Errorf("%s = %v, want %s", what, got, want)
	}
}
