// Command gen-oui downloads the IEEE MA-L, MA-M and MA-S registries and writes the compressed
// prefix table embedded by internal/oui. It runs at development/release time only
// (go generate ./internal/oui); the server never downloads anything at runtime (FR-016).
package main

import (
	"compress/gzip"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type source struct {
	url  string
	bits int
}

var sources = []source{
	{"https://standards-oui.ieee.org/oui/oui.csv", 24},
	{"https://standards-oui.ieee.org/oui28/mam.csv", 28},
	{"https://standards-oui.ieee.org/oui36/oui36.csv", 36},
}

type entry struct {
	bits   int
	prefix string
	vendor string
}

func main() {
	out := flag.String("out", "oui.tsv.gz", "output file")
	flag.Parse()

	client := &http.Client{Timeout: 2 * time.Minute}
	var entries []entry
	for _, s := range sources {
		es, err := fetch(client, s)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gen-oui:", err)
			os.Exit(1)
		}
		entries = append(entries, es...)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].bits != entries[j].bits {
			return entries[i].bits < entries[j].bits
		}
		return entries[i].prefix < entries[j].prefix
	})
	if err := write(*out, entries); err != nil {
		fmt.Fprintln(os.Stderr, "gen-oui:", err)
		os.Exit(1)
	}
	fmt.Printf("gen-oui: wrote %d entries to %s\n", len(entries), *out)
}

func fetch(client *http.Client, s source) ([]entry, error) {
	req, err := http.NewRequest(http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}
	// The IEEE site rejects requests without a browser-like user agent.
	req.Header.Set("User-Agent", "Mozilla/5.0 (home-net-explorer gen-oui)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", s.url, resp.Status)
	}

	r := csv.NewReader(resp.Body)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	if _, err := r.Read(); err != nil { // header: Registry,Assignment,Organization Name,...
		return nil, fmt.Errorf("%s: %w", s.url, err)
	}
	var out []entry
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.url, err)
		}
		if len(rec) < 3 {
			continue
		}
		prefix := strings.ToLower(strings.TrimSpace(rec[1]))
		vendor := strings.Join(strings.Fields(rec[2]), " ")
		if len(prefix)*4 != s.bits || vendor == "" {
			continue
		}
		out = append(out, entry{bits: s.bits, prefix: prefix, vendor: vendor})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no entries parsed", s.url)
	}
	return out, nil
}

// write produces a deterministic gzip file (no name or timestamp in the header), one
// "bits<TAB>prefix-hex<TAB>vendor" line per entry.
func write(path string, entries []entry) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		f.Close()
		return err
	}
	for _, e := range entries {
		if _, err := fmt.Fprintf(zw, "%d\t%s\t%s\n", e.bits, e.prefix, e.vendor); err != nil {
			f.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
