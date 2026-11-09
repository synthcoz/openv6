// Package v6 contains helpers to compose IPv6 addresses from a pool prefix,
// a subnet index and a 64-bit interface identifier (suffix).
package v6

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"strconv"
)

// Compose builds an address: prefix bits from pfx, then subnet index placed in
// bits [pfx.Bits(), 64), then the 64-bit suffix in the lower half.
func Compose(pfx netip.Prefix, subnet uint64, suffix uint64) (netip.Addr, error) {
	if !pfx.Addr().Is6() || pfx.Addr().Is4In6() {
		return netip.Addr{}, errors.New("prefix is not IPv6")
	}
	bits := pfx.Bits()
	if bits > 64 || bits < 1 {
		return netip.Addr{}, fmt.Errorf("prefix length /%d not supported (need /1../64)", bits)
	}
	if max := SubnetCount(pfx); max != 0 && subnet >= max {
		return netip.Addr{}, fmt.Errorf("subnet index %d out of range (max %d)", subnet, max-1)
	}
	b := pfx.Masked().Addr().As16()
	hi := binary.BigEndian.Uint64(b[:8])
	hi |= subnet
	binary.BigEndian.PutUint64(b[:8], hi)
	binary.BigEndian.PutUint64(b[8:], suffix)
	return netip.AddrFrom16(b), nil
}

// SubnetCount returns how many /64 networks are inside pfx. Returns 0 when the
// count does not fit in uint64 (prefix shorter than /1, not used in practice).
func SubnetCount(pfx netip.Prefix) uint64 {
	n := 64 - pfx.Bits()
	if n >= 64 {
		return 0
	}
	return 1 << uint(n)
}

// Slash64 returns the /64 containing the address.
func Slash64(a netip.Addr) netip.Prefix {
	p, _ := a.Prefix(64)
	return p
}

// RandomSuffix returns a random interface identifier which avoids small values
// (::1, ::2 … are usually taken by routers) and the all-ones value.
func RandomSuffix() uint64 {
	for {
		var b [8]byte
		_, _ = rand.Read(b[:])
		v := binary.BigEndian.Uint64(b[:])
		if v > 0xffff && v != ^uint64(0) {
			return v
		}
	}
}

// RandomSubnet returns a random subnet index in [0, count).
func RandomSubnet(count uint64) uint64 {
	if count <= 1 {
		return 0
	}
	n, _ := rand.Int(rand.Reader, new(big.Int).SetUint64(count))
	return n.Uint64()
}

func FormatSuffix(v uint64) string { return fmt.Sprintf("%016x", v) }

func ParseSuffix(s string) (uint64, error) { return strconv.ParseUint(s, 16, 64) }
