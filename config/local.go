// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"net"
	"strings"

	"github.com/omec-project/upfadapter/types"
)

// LocalN4 is the adapter's own N4 identity: the Node ID and the addresses it puts in the
// messages it relays in place of the SMF's, so the UPF answers and reports to the adapter.
type LocalN4 struct {
	IPv4   net.IP
	IPv6   net.IP
	NodeID types.NodeID
}

// AdapterN4 is the adapter's N4 identity, set at start from the environment (see
// resolveLocalN4).
var AdapterN4 LocalN4

// resolveLocalN4 derives the adapter's N4 identity from its environment, the same way the
// SMF derives its own from pfcp.addr:
//
//   - podIPs (POD_IPS, from status.podIPs) is a comma-separated address list, one per family;
//     otherwise podIP (POD_IP) is the one address. The first address of each family is the
//     adapter's N4 address in that family.
//   - nodeID (UPFADAPTER_NODE_ID), when set, is the Node ID: an IPv4 or IPv6 address or an
//     FQDN. Otherwise it is the N4 address of the preferred family, or the first listed.
//   - family (UPFADAPTER_PREFERRED_FAMILY, "ipv4" or "ipv6") also orders UPF name
//     resolution. Unset, IPv4 is preferred unless the adapter has no IPv4 address.
//
// With an IPv4 POD_IP, the default chart, the result is what the adapter used before. It
// used to take POD_IP through To4(), so on an IPv6 pod it had no address at all: it sent the
// Node ID 0.0.0.0 and an F-SEID with no address, and the UPF could not reach it.
func resolveLocalN4(podIP, podIPs, nodeID, family string) (LocalN4, types.AddressFamily, error) {
	var local LocalN4

	preferred, err := types.ParseAddressFamily(family)
	if err != nil {
		return local, types.AddressFamilyAuto, err
	}

	value := podIP
	if strings.TrimSpace(podIPs) != "" {
		value = podIPs
	}
	var first net.IP
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		ip := net.ParseIP(strings.TrimSuffix(strings.TrimPrefix(entry, "["), "]"))
		if ip == nil {
			return local, types.AddressFamilyAuto, fmt.Errorf("pod address %q is not an IP address", entry)
		}
		if first == nil {
			first = ip
		}
		if v4 := ip.To4(); v4 != nil {
			if local.IPv4 == nil {
				local.IPv4 = v4
			}
		} else if local.IPv6 == nil {
			local.IPv6 = ip.To16()
		}
	}

	resolveFamily := preferred
	if preferred == types.AddressFamilyAuto && local.IPv4 == nil && local.IPv6 != nil {
		resolveFamily = types.AddressFamilyIPv6
	}

	switch {
	case nodeID != "":
		local.NodeID = *types.NewNodeID(strings.TrimSuffix(strings.TrimPrefix(nodeID, "["), "]"))
	case preferred == types.AddressFamilyIPv6 && local.IPv6 != nil:
		local.NodeID = *types.NewNodeID(local.IPv6.String())
	case preferred == types.AddressFamilyIPv4 && local.IPv4 != nil:
		local.NodeID = *types.NewNodeID(local.IPv4.String())
	case first != nil:
		local.NodeID = *types.NewNodeID(first.String())
	default:
		// No address: an IPv4 Node ID with no value, which is what the adapter had before.
		local.NodeID = types.NodeID{NodeIdType: types.NodeIdTypeIpv4Address}
	}
	return local, resolveFamily, nil
}

// primaryIP is the address UpfAdapterIp has always held: the adapter's address in its Node
// ID's family, or its only address.
func (l LocalN4) primaryIP() net.IP {
	switch {
	case l.NodeID.NodeIdType == types.NodeIdTypeIpv6Address && l.IPv6 != nil:
		return l.IPv6
	case l.IPv4 != nil:
		return l.IPv4
	default:
		return l.IPv6
	}
}

// ListenAddr is the address the PFCP socket binds: the adapter's address when it has one,
// and the wildcard when it has one per family, which Go opens as a dual-stack socket.
func (l LocalN4) ListenAddr(port int) *net.UDPAddr {
	addr := &net.UDPAddr{Port: port}
	if l.IPv4 == nil || l.IPv6 == nil {
		addr.IP = l.primaryIP()
	}
	return addr
}
