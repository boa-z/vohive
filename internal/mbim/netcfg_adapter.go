package mbimcore

import (
	"net"

	"github.com/boa-z/quectel-qmi-go/pkg/netcfg"
)

type netConfigurator interface {
	SetIPv4(iface, addr string, prefix int) error
	SetIPv6(iface, addr string, prefix int) error
	SetMTU(iface string, mtu int) error
	BringUp(iface string) error
	AddDefaultRoute(iface, gateway string) error
	SetDNS(dns []string) error
	Flush(iface string) error
}

type realNetConfigurator struct{}

func (realNetConfigurator) SetIPv4(iface, addr string, prefix int) error {
	return netcfg.SetIPAddress(iface, net.ParseIP(addr), prefix)
}

func (realNetConfigurator) SetIPv6(iface, addr string, prefix int) error {
	return netcfg.SetIPv6Address(iface, net.ParseIP(addr), prefix)
}

func (realNetConfigurator) SetMTU(iface string, mtu int) error { return netcfg.SetMTU(iface, mtu) }
func (realNetConfigurator) BringUp(iface string) error         { return netcfg.BringUp(iface) }

func (realNetConfigurator) AddDefaultRoute(iface, gateway string) error {
	return netcfg.AddDefaultRoute(iface, net.ParseIP(gateway))
}

func (realNetConfigurator) SetDNS(dns []string) error {
	var dns1, dns2 string
	if len(dns) > 0 {
		dns1 = dns[0]
	}
	if len(dns) > 1 {
		dns2 = dns[1]
	}
	return netcfg.UpdateResolvConf(dns1, dns2)
}

func (realNetConfigurator) Flush(iface string) error { return netcfg.FlushAddresses(iface) }
