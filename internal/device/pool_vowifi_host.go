package device

import "github.com/zanescope/vohive/internal/vowifihost"

func (p *Pool) voWiFiHost() *vowifihost.Manager {
	if p == nil {
		return vowifihost.NewManager()
	}
	if p.vowifiHost == nil {
		p.vowifiHost = vowifihost.NewManager()
	}
	return p.vowifiHost
}
