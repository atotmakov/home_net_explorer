package oui

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strconv"
	"strings"
	"sync"
)

//go:embed oui.tsv.gz
var data []byte

type table struct {
	byBits map[int]map[uint64]string // prefix length → prefix value → vendor
}

var load = sync.OnceValue(func() *table {
	t := &table{byBits: map[int]map[uint64]string{24: {}, 28: {}, 36: {}}}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		panic("oui: corrupt embedded table: " + err.Error())
	}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		bits, rest, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			continue
		}
		hex, vendor, ok := strings.Cut(rest, "\t")
		if !ok {
			continue
		}
		n, err1 := strconv.Atoi(bits)
		v, err2 := strconv.ParseUint(hex, 16, 64)
		if err1 != nil || err2 != nil || t.byBits[n] == nil {
			continue
		}
		t.byBits[n][v] = vendor
	}
	return t
})

// parse turns aa:bb:cc:dd:ee:ff into a 48-bit number.
func parse(mac string) (uint64, bool) {
	hex := strings.ReplaceAll(strings.ReplaceAll(mac, ":", ""), "-", "")
	if len(hex) != 12 {
		return 0, false
	}
	v, err := strconv.ParseUint(hex, 16, 64)
	return v, err == nil
}

// Lookup returns the registered owner of mac's prefix (longest match wins), or "".
func Lookup(mac string) string {
	v, ok := parse(mac)
	if !ok {
		return ""
	}
	t := load()
	for _, bits := range []int{36, 28, 24} {
		if vendor, ok := t.byBits[bits][v>>uint(48-bits)]; ok {
			return vendor
		}
	}
	return ""
}

// IsRandomized reports whether mac has the locally administered bit set (second-lowest bit of
// the first byte), as private/randomized MACs on phones and laptops do.
func IsRandomized(mac string) bool {
	v, ok := parse(mac)
	return ok && (v>>40)&0x02 != 0
}

// Manufacturer is Lookup, except randomized MACs have no manufacturer (data-model.md).
func Manufacturer(mac string) string {
	if IsRandomized(mac) {
		return ""
	}
	return Lookup(mac)
}
