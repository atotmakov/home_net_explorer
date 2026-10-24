package inventory_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
)

// Feature 012: an access point's client is a sighting with the Wi-Fi network in via and the
// access point's address in via_source; it never completes a scan of the subnet.

func apClient(ip, mac, via, ap string) contract.Observation {
	return contract.Observation{IP: ip, MAC: mac, Via: via, AccessPoint: ap, Method: contract.ObsAPClient}
}

type apSighting struct {
	Via, Source string
	Count       int
}

func apSightings(t *testing.T, h *it.Harness, mac string) []apSighting {
	t.Helper()
	rows, err := h.Store.DB().Query(`SELECT via, via_source, seen_count FROM sightings WHERE mac = ? ORDER BY id`, mac)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []apSighting
	for rows.Next() {
		var s apSighting
		if err := rows.Scan(&s.Via, &s.Source, &s.Count); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestAPClientSighting(t *testing.T) {
	h := it.New(t)
	const phone = "00:00:5e:10:00:13"
	// The router read covers the subnet (complete); the AP lists the phone.
	routerAt(h, 0, routerScan(true), apClient("192.168.0.13", phone, "Home (5 GHz)", "192.168.0.5"))
	if got := devices(t, h); len(got) != 1 || got[0].IdentityKey != "mac:"+phone {
		t.Fatalf("devices = %+v, want the phone", got)
	}
	want := []apSighting{{"Home (5 GHz)", "192.168.0.5", 1}}
	if got := apSightings(t, h, phone); !reflect.DeepEqual(got, want) {
		t.Fatalf("sightings = %+v, want %+v", got, want)
	}

	// Same AP, same network: the same sighting row grows.
	routerAt(h, 15, routerScan(true), apClient("192.168.0.13", phone, "Home (5 GHz)", "192.168.0.5"))
	want[0].Count = 2
	if got := apSightings(t, h, phone); !reflect.DeepEqual(got, want) {
		t.Fatalf("after a second scan sightings = %+v, want %+v", got, want)
	}

	// The other access point: a new row (the via text is the same, the source is not).
	routerAt(h, 30, routerScan(true), apClient("192.168.0.13", phone, "Home (5 GHz)", "192.168.0.10"))
	want = append(want, apSighting{"Home (5 GHz)", "192.168.0.10", 1})
	if got := apSightings(t, h, phone); !reflect.DeepEqual(got, want) {
		t.Fatalf("after roaming sightings = %+v, want %+v", got, want)
	}

	// A rebuild from the stored runs gives the same rows (Principle V).
	if err := inventory.Rebuild(context.Background(), h.Store, h.Applier); err != nil {
		t.Fatal(err)
	}
	if got := apSightings(t, h, phone); !reflect.DeepEqual(got, want) {
		t.Errorf("after a rebuild sightings = %+v, want %+v", got, want)
	}
}

// An ap_client observation in a subnet scanned only partially is presence, never a completed
// scan: a device the access point does not list is not marked offline (spec FR-008).
func TestAPClientNeverMarksOffline(t *testing.T) {
	h := it.New(t)
	const tv, phone = "00:00:5e:10:00:05", "00:00:5e:10:00:13"
	routerAt(h, 0, routerScan(true), listed("192.168.0.5", tv, "tv", "LAN1"), listed("192.168.0.13", phone, "", "LAN1"))
	// Later runs: the router read failed (subnet listed, not complete); only the AP lists the phone.
	for m := 15; m <= 45; m += 15 {
		routerAt(h, m, routerScan(false), apClient("192.168.0.13", phone, "Home (5 GHz)", "192.168.0.5"))
	}
	for _, d := range devices(t, h) {
		if d.Status == "offline" {
			t.Errorf("%s status = %s, want online (an AP list is not a completed scan)", d.IdentityKey, d.Status)
		}
	}
}
