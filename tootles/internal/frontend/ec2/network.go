package ec2

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"github.com/tinkerbell/tinkerbell/tootles/internal/ginutil"
)

// networkInterfaceLeaves lists the AWS-IMDS-compatible leaf fields served
// under /meta-data/network/interfaces/macs/{mac}/. Each entry produces the
// response body for a single GET. The bool return distinguishes "unset"
// (→ 404 from the handler) from "set to zero value" (→ 200 with empty body).
var networkInterfaceLeaves = []struct {
	Name string
	Get  func(data.NetworkInterface) (string, bool)
}{
	{"device-number", func(n data.NetworkInterface) (string, bool) {
		if n.DeviceNumber == nil {
			return "", false
		}
		return strconv.FormatInt(*n.DeviceNumber, 10), true
	}},
	{"interface-id", func(n data.NetworkInterface) (string, bool) {
		if n.InterfaceID == nil {
			return "", false
		}
		return *n.InterfaceID, true
	}},
	{"ipv6s", func(n data.NetworkInterface) (string, bool) {
		if len(n.IPv6s) == 0 {
			return "", false
		}
		return strings.Join(n.IPv6s, "\n"), true
	}},
	{"local-hostname", func(n data.NetworkInterface) (string, bool) {
		if n.LocalHostname == nil {
			return "", false
		}
		return *n.LocalHostname, true
	}},
	{"local-ipv4s", func(n data.NetworkInterface) (string, bool) {
		if len(n.LocalIPv4s) == 0 {
			return "", false
		}
		return strings.Join(n.LocalIPv4s, "\n"), true
	}},
	{"mac", func(n data.NetworkInterface) (string, bool) {
		if n.Mac == nil {
			return "", false
		}
		return *n.Mac, true
	}},
	{"public-hostname", func(n data.NetworkInterface) (string, bool) {
		if n.PublicHostname == nil {
			return "", false
		}
		return *n.PublicHostname, true
	}},
	{"public-ipv4s", func(n data.NetworkInterface) (string, bool) {
		if len(n.PublicIPv4s) == 0 {
			return "", false
		}
		return strings.Join(n.PublicIPv4s, "\n"), true
	}},
	{"subnet-ipv4-cidr-block", func(n data.NetworkInterface) (string, bool) {
		if n.SubnetIPv4CidrBlock == nil {
			return "", false
		}
		return *n.SubnetIPv4CidrBlock, true
	}},
	{"subnet-ipv6-cidr-blocks", func(n data.NetworkInterface) (string, bool) {
		if len(n.SubnetIPv6CidrBlocks) == 0 {
			return "", false
		}
		return strings.Join(n.SubnetIPv6CidrBlocks, "\n"), true
	}},
	{"vpc-ipv4-cidr-blocks", func(n data.NetworkInterface) (string, bool) {
		if len(n.VpcIPv4CidrBlocks) == 0 {
			return "", false
		}
		return strings.Join(n.VpcIPv4CidrBlocks, "\n"), true
	}},
	{"vpc-ipv6-cidr-blocks", func(n data.NetworkInterface) (string, bool) {
		if len(n.VpcIPv6CidrBlocks) == 0 {
			return "", false
		}
		return strings.Join(n.VpcIPv6CidrBlocks, "\n"), true
	}},
}

// leafValue returns (value, true) if the named leaf is set on n, or ("", false) otherwise.
func leafValue(n data.NetworkInterface, name string) (string, bool) {
	for _, l := range networkInterfaceLeaves {
		if l.Name == name {
			return l.Get(n)
		}
	}
	return "", false
}

// leafListing returns a newline-joined listing of leaf names that are set on n.
// Output is sorted alphabetically to match AWS IMDS behaviour.
func leafListing(n data.NetworkInterface) string {
	var out []string
	for _, l := range networkInterfaceLeaves {
		if _, ok := l.Get(n); ok {
			out = append(out, l.Name)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// macListing returns a newline-joined listing of MAC keys from the network
// config, each with a trailing slash to signal they are directories in IMDS.
// Output is sorted alphabetically.
func macListing(net data.InstanceNetwork) string {
	keys := make([]string, 0, len(net.Interfaces))
	for mac := range net.Interfaces {
		keys = append(keys, mac+"/")
	}
	sort.Strings(keys)
	return strings.Join(keys, "\n")
}

// lookupInterface returns the interface for the given mac, using a
// case-insensitive match. The backend stores MAC keys lowercased; accepting
// mixed case on the URL side matches AWS IMDS leniency.
func lookupInterface(net data.InstanceNetwork, mac string) (data.NetworkInterface, bool) {
	iface, ok := net.Interfaces[strings.ToLower(mac)]
	return iface, ok
}

// configureNetworkRoutes registers all routes under /meta-data/network/ on
// the given router. The subtree has three fixed-depth levels plus two dynamic
// per-MAC levels:
//
//	/meta-data/network                                 → "interfaces/"
//	/meta-data/network/interfaces                      → "macs/"
//	/meta-data/network/interfaces/macs                 → sorted list of "{mac}/"
//	/meta-data/network/interfaces/macs/:mac            → sorted leaf names
//	/meta-data/network/interfaces/macs/:mac/:leaf      → leaf value
//
// viaInstanceID selects between IP-based and instanceID-based instance lookup.
// The caller is expected to register the handler once per supported prefix.
func (f Frontend) configureNetworkRoutes(router ginutil.TrailingSlashRouteHelper, viaInstanceID bool) {
	const base = "/meta-data/network"

	resolve := func(ctx *gin.Context) (data.Ec2Instance, error) {
		if viaInstanceID {
			return f.getInstanceViaInstanceID(ctx)
		}
		return f.getInstanceViaIP(ctx, ctx.Request)
	}

	router.GET(base, func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "interfaces/")
	})

	router.GET(base+"/interfaces", func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "macs/")
	})

	router.GET(base+"/interfaces/macs", func(ctx *gin.Context) {
		instance, err := resolve(ctx)
		if err != nil {
			f.writeInstanceDataOrErrToHTTP(ctx, err, "")
			return
		}
		ctx.String(http.StatusOK, macListing(instance.Metadata.Network))
	})

	router.GET(base+"/interfaces/macs/:mac", func(ctx *gin.Context) {
		instance, err := resolve(ctx)
		if err != nil {
			f.writeInstanceDataOrErrToHTTP(ctx, err, "")
			return
		}
		iface, ok := lookupInterface(instance.Metadata.Network, ctx.Param("mac"))
		if !ok {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		ctx.String(http.StatusOK, leafListing(iface))
	})

	router.GET(base+"/interfaces/macs/:mac/:leaf", func(ctx *gin.Context) {
		instance, err := resolve(ctx)
		if err != nil {
			f.writeInstanceDataOrErrToHTTP(ctx, err, "")
			return
		}
		iface, ok := lookupInterface(instance.Metadata.Network, ctx.Param("mac"))
		if !ok {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		value, ok := leafValue(iface, ctx.Param("leaf"))
		if !ok {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		ctx.String(http.StatusOK, value)
	})
}
