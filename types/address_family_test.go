// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"context"
	"net"
	"testing"
)

func fakeDNS(t *testing.T, answers map[string][]string) {
	t.Helper()
	restore := SetIPLookupForTest(func(_ context.Context, _, host string) ([]net.IP, error) {
		var ips []net.IP
		for _, s := range answers[host] {
			ips = append(ips, net.ParseIP(s))
		}
		if len(ips) == 0 {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		return ips, nil
	})
	t.Cleanup(restore)
}

func preferFamily(t *testing.T, f AddressFamily) {
	t.Helper()
	prev := AddressFamily(preferredFamily.Load())
	SetPreferredFamily(f)
	t.Cleanup(func() { SetPreferredFamily(prev) })
}

func forgetHost(t *testing.T, hosts ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, h := range hosts {
			deleteDnsHost(h)
		}
	})
}

func TestParseAddressFamily(t *testing.T) {
	for in, want := range map[string]AddressFamily{
		"": AddressFamilyAuto, "auto": AddressFamilyAuto, "IPv4": AddressFamilyIPv4, "ipv6": AddressFamilyIPv6,
	} {
		if got, err := ParseAddressFamily(in); err != nil || got != want {
			t.Errorf("ParseAddressFamily(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseAddressFamily("inet6"); err == nil {
		t.Error("an unknown family was accepted")
	}
}

func TestSelectAddress(t *testing.T) {
	v4, v6 := net.ParseIP("192.0.2.1"), net.ParseIP("2001:db8::1")
	for _, tc := range []struct {
		name      string
		ips       []net.IP
		want      net.IP
		preferred AddressFamily
	}{
		{"auto is IPv4 first", []net.IP{v6, v4}, v4, AddressFamilyAuto},
		{"IPv6 preferred", []net.IP{v4, v6}, v6, AddressFamilyIPv6},
		{"falls back to the other family", []net.IP{v4.To4()}, v4, AddressFamilyIPv6},
		{"empty", nil, nil, AddressFamilyIPv4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectAddress(tc.ips, tc.preferred)
			if !got.Equal(tc.want) || (got != nil && len(got) != net.IPv6len) {
				t.Errorf("SelectAddress = %v (%d bytes), want %v in 16-byte form", got, len(got), tc.want)
			}
		})
	}
}

func TestResolveIP(t *testing.T) {
	fakeDNS(t, map[string][]string{"upf.dual.test": {"2001:db8::7", "192.0.2.7"}})
	forgetHost(t, "upf.dual.test", "upf.missing.test")

	preferFamily(t, AddressFamilyAuto)
	if ip, err := NewNodeID("upf.dual.test").ResolveIP(); err != nil || !ip.Equal(net.ParseIP("192.0.2.7")) {
		t.Errorf("ResolveIP = %v, %v; want 192.0.2.7", ip, err)
	}
	deleteDnsHost("upf.dual.test")
	SetPreferredFamily(AddressFamilyIPv6)
	if ip, err := NewNodeID("upf.dual.test").ResolveIP(); err != nil || !ip.Equal(net.ParseIP("2001:db8::7")) {
		t.Errorf("ResolveIP preferring IPv6 = %v, %v; want 2001:db8::7", ip, err)
	}

	// A name that does not resolve is an error and nil, not 0.0.0.0.
	missing := NewNodeID("upf.missing.test")
	if ip, err := missing.ResolveIP(); err == nil {
		t.Errorf("ResolveIP of an unresolvable name = %v, want an error", ip)
	}
	if ip := missing.ResolveNodeIdToIp(); ip != nil {
		t.Errorf("ResolveNodeIdToIp of an unresolvable name = %v, want nil", ip)
	}
}

func TestValueString(t *testing.T) {
	for in, want := range map[string]string{"10.0.0.1": "10.0.0.1", "2001:db8::1": "2001:db8::1", "upf.example": "upf.example"} {
		if got := NewNodeID(in).ValueString(); got != want {
			t.Errorf("ValueString(%q) = %q, want %q", in, got, want)
		}
	}
}
