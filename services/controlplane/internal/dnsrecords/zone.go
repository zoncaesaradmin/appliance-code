package dnsrecords

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// RenderZoneFile builds a BIND-style zone file for the appliance LAN zone.
func RenderZoneFile(zone string, records []storage.DNSRecord, serial int64, nsIPv4 string) string {
	zone = strings.TrimSuffix(strings.TrimSpace(zone), ".")
	if serial <= 0 {
		serial = time.Now().UTC().Unix()
	}
	if nsIPv4 == "" {
		nsIPv4 = "127.0.0.1"
		for _, rec := range records {
			if rec.IPv4 != "" {
				nsIPv4 = rec.IPv4
				break
			}
		}
	}
	sorted := append([]storage.DNSRecord(nil), records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	fmt.Fprintf(&b, "$ORIGIN %s.\n", zone)
	fmt.Fprintf(&b, "@   3600 IN SOA ns.%s. hostmaster.%s. (\n", zone, zone)
	fmt.Fprintf(&b, "        %d 7200 3600 1209600 3600 )\n", serial)
	fmt.Fprintf(&b, "    3600 IN NS ns.%s.\n", zone)
	fmt.Fprintf(&b, "ns  3600 IN A %s\n", nsIPv4)
	for _, rec := range sorted {
		name := strings.TrimSpace(rec.Name)
		if name == "" || name == "ns" {
			continue
		}
		ttl := rec.TTL
		if ttl <= 0 {
			ttl = DefaultAdminTTL
		}
		fmt.Fprintf(&b, "%s %d IN A %s\n", name, ttl, rec.IPv4)
	}
	return b.String()
}
