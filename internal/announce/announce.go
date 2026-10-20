// Package announce makes the agent findable on the local network: it
// announces the service _mira._tcp by mDNS, and the Mira integration in
// Home Assistant lists every agent it hears under "Discovered".
//
// The TXT record carries the agent's instance id, so Home Assistant
// recognises an agent it already knows even when its address changes.
package announce

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/libp2p/zeroconf/v2"
)

// Service is the mDNS service type; custom_components/mira/manifest.json
// lists the same one.
const Service = "_mira._tcp"

type Options struct {
	// Instance is the visible name, e.g. "Mira".
	Instance string
	ID       string
	Version  string
	// Listen is the HTTP API's listen address; its port is announced.
	Listen string
	Logger *slog.Logger
}

// Run announces the agent until ctx ends.
func Run(ctx context.Context, opts Options) error {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	port, err := listenPort(opts.Listen)
	if err != nil {
		return err
	}
	txt := []string{"id=" + opts.ID, "version=" + opts.Version, "path=/v1"}
	ifaces, ips := lanInterfaces()
	if len(ips) == 0 {
		return fmt.Errorf("mdns: no network interface with an address")
	}
	// A host name of its own, with exactly these addresses. Under the
	// machine's name, avahi would answer too, with every address of the
	// host, container bridges included.
	host := "mira"
	if h, err := os.Hostname(); err == nil && h != "" {
		host = "mira-" + strings.ToLower(strings.Split(h, ".")[0])
	}
	server, err := zeroconf.RegisterProxy(opts.Instance, Service, "local.", port, host, ips, txt, ifaces)
	if err != nil {
		return fmt.Errorf("mdns: %w", err)
	}
	opts.Logger.Info("announcing on the local network", "component", "announce", "service", Service, "port", port, "id", opts.ID, "addresses", ips)
	<-ctx.Done()
	server.Shutdown()
	return ctx.Err()
}

// virtualPrefixes are interfaces of containers and VMs. Their addresses are
// unreachable from Home Assistant on another host; announced first, they
// would be the one it picks.
var virtualPrefixes = []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "podman", "lxc", "vnet", "tun", "tap", "zt", "tailscale", "wg"}

// announcable says whether an address is one Home Assistant should be pointed
// at: a plain IPv4 address of the local network. IPv6 addresses are left out
// on purpose: most are temporary privacy addresses that change by the day, so
// an entry that kept one lost the agent when it expired. The agent
// announces once at start; an address that lives only hours would not do.
func announcable(ip net.IP) bool {
	return ip.To4() != nil && !ip.IsLinkLocalUnicast() && !ip.IsLoopback()
}

// lanInterfaces are the interfaces that are up, can multicast and are not
// virtual, with their IPv4 addresses.
func lanInterfaces() ([]net.Interface, []string) {
	all, err := net.Interfaces()
	if err != nil {
		return nil, nil
	}
	var out []net.Interface
	var ips []string
	for _, ifc := range all {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagMulticast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		if slices.ContainsFunc(virtualPrefixes, func(p string) bool { return strings.HasPrefix(ifc.Name, p) }) {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		before := len(ips)
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || !announcable(ipnet.IP) {
				continue
			}
			ips = append(ips, ipnet.IP.String())
		}
		if len(ips) > before {
			out = append(out, ifc)
		}
	}
	return out, ips
}

func listenPort(listen string) (int, error) {
	_, p, err := net.SplitHostPort(listen)
	if err != nil {
		return 0, fmt.Errorf("server.listen %q: %w", listen, err)
	}
	port, err := strconv.Atoi(p)
	if err != nil || port <= 0 {
		return 0, fmt.Errorf("server.listen %q has no fixed port", listen)
	}
	return port, nil
}

// Loopback reports whether listen binds only to loopback, where announcing
// would point Home Assistant at an address it cannot reach.
func Loopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
