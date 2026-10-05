// Package contracttest gives tests access to the contract fixtures in tests/contract/fixtures
// and to the JSON schemas in contracts/collector-upload-api.yaml. It is imported only by tests.
package contracttest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// Root returns the repository root.
func Root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// FixturePath returns the path of a fixture file by base name.
func FixturePath(name string) string {
	return filepath.Join(Root(), "tests", "contract", "fixtures", name)
}

// OpenAPIPath returns the path of the collector upload OpenAPI document.
func OpenAPIPath() string {
	return filepath.Join(Root(), "specs", "001-lan-inventory-topology", "contracts", "collector-upload-api.yaml")
}

// Fixture reads a fixture file.
func Fixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(FixturePath(name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

// Fixtures lists fixture base names starting with prefix, sorted.
func Fixtures(t testing.TB, prefix string) []string {
	t.Helper()
	paths, err := filepath.Glob(FixturePath(prefix + "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatalf("no fixtures with prefix %q", prefix)
	}
	return names
}

// TooManyObservations returns valid_minimal.json with 4097 observations (one over the limit).
func TooManyObservations(t testing.TB) []byte {
	t.Helper()
	m := decode(t, Fixture(t, "valid_minimal.json"))
	first := m["observations"].([]any)[0]
	obs := make([]any, 0, 4097)
	for i := 0; i < 4097; i++ {
		obs = append(obs, first)
	}
	m["observations"] = obs
	return encode(t, m)
}

// TooManySubnets returns valid_minimal.json with 17 subnets (one over the limit).
func TooManySubnets(t testing.TB) []byte {
	t.Helper()
	m := decode(t, Fixture(t, "valid_minimal.json"))
	subnets := m["subnets"].([]any)
	for i := 0; len(subnets) < 17; i++ {
		subnets = append(subnets, map[string]any{
			"cidr":         fmt.Sprintf("10.0.%d.0/24", i),
			"method":       "arp",
			"complete":     true,
			"hosts_probed": 254,
		})
	}
	m["subnets"] = subnets
	return encode(t, m)
}

// Modify decodes a fixture into a generic map, applies f, and re-encodes it.
func Modify(t testing.TB, name string, f func(m map[string]any)) []byte {
	t.Helper()
	m := decode(t, Fixture(t, name))
	f(m)
	return encode(t, m)
}

func decode(t testing.TB, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func encode(t testing.TB, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var (
	schemaOnce sync.Once
	schemaErr  error
	compiler   *jsonschema.Compiler
	compiledMu sync.Mutex
	compiled   = map[string]*jsonschema.Schema{}
)

const schemaURL = "file:///collector-upload-api.json"

// Schema compiles components.schemas[name] from the OpenAPI document.
func Schema(t testing.TB, name string) *jsonschema.Schema {
	t.Helper()
	schemaOnce.Do(loadSchemas)
	if schemaErr != nil {
		t.Fatalf("load OpenAPI schemas: %v", schemaErr)
	}
	compiledMu.Lock()
	defer compiledMu.Unlock()
	if s, ok := compiled[name]; ok {
		return s
	}
	s, err := compiler.Compile(schemaURL + "#/$defs/" + name)
	if err != nil {
		t.Fatalf("compile schema %s: %v", name, err)
	}
	compiled[name] = s
	return s
}

// Validate checks a JSON document against a compiled schema.
func Validate(s *jsonschema.Schema, data []byte) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return s.Validate(inst)
}

// loadSchemas turns components.schemas into a standalone JSON Schema document under $defs,
// rewriting "#/components/schemas/X" references to "#/$defs/X".
func loadSchemas() {
	raw, err := os.ReadFile(OpenAPIPath())
	if err != nil {
		schemaErr = err
		return
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		schemaErr = err
		return
	}
	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	if len(schemas) == 0 {
		schemaErr = fmt.Errorf("no components.schemas in %s", OpenAPIPath())
		return
	}
	j, err := json.Marshal(map[string]any{"$defs": rewriteRefs(schemas)})
	if err != nil {
		schemaErr = err
		return
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		schemaErr = err
		return
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	if err := c.AddResource(schemaURL, inst); err != nil {
		schemaErr = err
		return
	}
	compiler = c
}

func rewriteRefs(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			if s, ok := vv.(string); ok && k == "$ref" {
				out[k] = strings.Replace(s, "#/components/schemas/", "#/$defs/", 1)
				continue
			}
			out[k] = rewriteRefs(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = rewriteRefs(vv)
		}
		return out
	default:
		return v
	}
}
