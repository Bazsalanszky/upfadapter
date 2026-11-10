// SPDX-License-Identifier: Apache-2.0
// Copyright 2024 Canonical Ltd.

package types

import (
	"fmt"
	"net"
	"time"

	"github.com/omec-project/upfadapter/logger"
)

const (
	NodeIdTypeIpv4Address uint8 = iota
	NodeIdTypeIpv6Address
	NodeIdTypeFqdn
)

type NodeID struct {
	NodeIdValue []byte
	NodeIdType  uint8 // 0x00001111
}

var dnsHostIpCache map[string]net.IP

func NewNodeID(nodeID string) *NodeID {
	ip := net.ParseIP(nodeID)
	if ip == nil {
		return &NodeID{
			NodeIdType:  NodeIdTypeFqdn,
			NodeIdValue: []byte(nodeID),
		}
	} else if ip.To4() != nil {
		return &NodeID{
			NodeIdType:  NodeIdTypeIpv4Address,
			NodeIdValue: ip.To4(),
		}
	} else {
		return &NodeID{
			NodeIdType:  NodeIdTypeIpv6Address,
			NodeIdValue: ip.To16(),
		}
	}
}

// ResolveNodeIdToIp returns the address of the node: the value itself for an IP Node ID,
// and the cached or freshly resolved address for an FQDN (see ResolveIP). It returns nil
// when the name does not resolve; it used to return 0.0.0.0, which callers could not tell
// apart from an address, and which the adapter then sent PFCP to.
func (n *NodeID) ResolveNodeIdToIp() net.IP {
	ip, err := n.ResolveIP()
	if err != nil {
		return nil
	}
	return ip
}

// ResolveIP is ResolveNodeIdToIp with the failure reported. When an FQDN has addresses of
// both families, the one returned is chosen by PreferredFamily (see SelectAddress), not by
// the order the resolver returns them in.
func (n *NodeID) ResolveIP() (net.IP, error) {
	switch n.NodeIdType {
	case NodeIdTypeIpv4Address, NodeIdTypeIpv6Address:
		return n.NodeIdValue, nil
	case NodeIdTypeFqdn:
		host := string(n.NodeIdValue)
		if ip, err := getDnsHostIp(host); err == nil {
			logger.AppLog.Debugf("host [%v] found in smf dns cache", host)
			return ip, nil
		}
		logger.AppLog.Warnf("host [%v] not found in smf dns cache", host)
		ip, err := lookupNodeAddress(host)
		if err != nil {
			logger.AppLog.Warnf("Host lookup failed: %+v", err)
			return nil, err
		}
		logger.AppLog.Infof("host [%v] dns resolved, updating smf dns cache", host)
		InsertDnsHostIp(host, ip)
		return ip, nil
	default:
		return nil, fmt.Errorf("unknown node id type %d", n.NodeIdType)
	}
}

// ValueString returns the Node ID as text in the form ie.NewNodeIDHeuristic maps back to
// the same Node ID type: the address of an IP Node ID, the name of an FQDN.
func (n *NodeID) ValueString() string {
	switch n.NodeIdType {
	case NodeIdTypeIpv4Address, NodeIdTypeIpv6Address:
		return net.IP(n.NodeIdValue).String()
	default:
		return string(n.NodeIdValue)
	}
}

func init() {
	dnsHostIpCache = make(map[string]net.IP)
	ticker := time.NewTicker(time.Minute)

	go func() {
		for {
			<-ticker.C
			RefreshDnsHostIpCache()
		}
	}()
}

func RefreshDnsHostIpCache() {
	for hostName := range dnsHostIpCache {
		logger.AppLog.Debugf("refreshing DNS for host [%v]", hostName)
		if ip, err := lookupNodeAddress(hostName); err != nil {
			logger.AppLog.Warnf("Host lookup failed: %+v", err)
			deleteDnsHost(hostName)
			continue
		} else if !dnsHostIpCache[hostName].Equal(ip) {
			logger.AppLog.Infof("smf dns cache updated for host [%v]: [%v]", hostName, ip.String())
			dnsHostIpCache[hostName] = ip
		}
	}
}

func getDnsHostIp(hostName string) (net.IP, error) {
	if ip, ok := dnsHostIpCache[hostName]; !ok {
		return nil, fmt.Errorf("host [%v] not found in smf dns cache", hostName)
	} else {
		return ip, nil
	}
}

func InsertDnsHostIp(hostName string, ip net.IP) {
	dnsHostIpCache[hostName] = ip
}

func deleteDnsHost(hostName string) {
	delete(dnsHostIpCache, hostName)
}
