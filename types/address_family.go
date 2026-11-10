// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
)

// AddressFamily names an IP address family. The zero value, AddressFamilyAuto, is no
// preference.
type AddressFamily uint8

const (
	AddressFamilyAuto AddressFamily = iota
	AddressFamilyIPv4
	AddressFamilyIPv6
)

// ParseAddressFamily reads "ipv4" or "ipv6", in any case, or "" / "auto" for no preference.
func ParseAddressFamily(s string) (AddressFamily, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return AddressFamilyAuto, nil
	case "ipv4":
		return AddressFamilyIPv4, nil
	case "ipv6":
		return AddressFamilyIPv6, nil
	default:
		return AddressFamilyAuto, fmt.Errorf("unknown address family %q (want ipv4, ipv6 or auto)", s)
	}
}

func (f AddressFamily) String() string {
	switch f {
	case AddressFamilyIPv4:
		return "ipv4"
	case AddressFamilyIPv6:
		return "ipv6"
	default:
		return "auto"
	}
}

// Has reports whether ip belongs to family f. AddressFamilyAuto matches any address.
func (f AddressFamily) Has(ip net.IP) bool {
	switch f {
	case AddressFamilyIPv4:
		return ip.To4() != nil
	case AddressFamilyIPv6:
		return ip.To4() == nil && ip.To16() != nil
	default:
		return ip != nil
	}
}

func (f AddressFamily) other() AddressFamily {
	switch f {
	case AddressFamilyIPv4:
		return AddressFamilyIPv6
	case AddressFamilyIPv6:
		return AddressFamilyIPv4
	default:
		return AddressFamilyAuto
	}
}

var preferredFamily atomic.Uint32

// SetPreferredFamily sets the family ResolveIP prefers. AddressFamilyAuto means IPv4.
func SetPreferredFamily(f AddressFamily) {
	preferredFamily.Store(uint32(f))
}

// PreferredFamily returns the family ResolveIP prefers; it is never AddressFamilyAuto.
func PreferredFamily() AddressFamily {
	if f := AddressFamily(preferredFamily.Load()); f == AddressFamilyIPv6 {
		return f
	}
	return AddressFamilyIPv4
}

// SelectAddress picks one address out of a resolver answer: the first of the preferred
// family, in the resolver's order, else the first of the other one, in the 16-byte form
// net.ParseIP returns. It returns nil for an empty answer. Taking the first entry, as the
// adapter used to, left the A-versus-AAAA choice to the resolver's RFC 6724 sort.
func SelectAddress(ips []net.IP, preferred AddressFamily) net.IP {
	if preferred == AddressFamilyAuto {
		preferred = AddressFamilyIPv4
	}
	for _, family := range []AddressFamily{preferred, preferred.other()} {
		for _, ip := range ips {
			if family.Has(ip) {
				return ip.To16()
			}
		}
	}
	return nil
}

type ipLookupFunc func(ctx context.Context, network, host string) ([]net.IP, error)

// ipLookupOverride, when set, answers instead of the system resolver, so tests can resolve
// names without DNS.
var ipLookupOverride atomic.Pointer[ipLookupFunc]

// SetIPLookupForTest makes f answer every name lookup until the returned function is called.
func SetIPLookupForTest(f func(ctx context.Context, network, host string) ([]net.IP, error)) (restore func()) {
	fn := ipLookupFunc(f)
	prev := ipLookupOverride.Swap(&fn)
	return func() { ipLookupOverride.Store(prev) }
}

func lookupNodeAddress(host string) (net.IP, error) {
	var (
		ips []net.IP
		err error
	)
	if f := ipLookupOverride.Load(); f != nil {
		ips, err = (*f)(context.Background(), "ip", host)
	} else {
		ips, err = net.DefaultResolver.LookupIP(context.Background(), "ip", host)
	}
	if err != nil {
		return nil, err
	}
	ip := SelectAddress(ips, PreferredFamily())
	if ip == nil {
		return nil, fmt.Errorf("host [%s] has no IP address", host)
	}
	return ip, nil
}
