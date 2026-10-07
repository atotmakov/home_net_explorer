package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/app"
	"github.com/atotmakov/home_net_explorer/internal/auth"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
)

type server struct {
	t     *testing.T
	app   *app.App
	srv   *httptest.Server
	token string
	id    int64
}

func newServer(t *testing.T) *server {
	t.Helper()
	a, err := app.New(context.Background(), app.Options{DataDir: filepath.Join(t.TempDir(), "data"), NoBuiltinScan: true})
	if err != nil {
		t.Fatal(err)
	}
	tok, id, err := auth.CreateCollector(context.Background(), a.Store.DB(), "desktop", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(func() { srv.Close(); a.Close() })
	return &server{t: t, app: a, srv: srv, token: tok, id: id}
}

func (s *server) do(method, path, token string, body []byte) (int, []byte) {
	s.t.Helper()
	req, _ := http.NewRequest(method, s.srv.URL+path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func expectError(t *testing.T, label string, status int, body []byte, wantStatus int, wantCode string) {
	t.Helper()
	if status != wantStatus {
		t.Errorf("%s: status %d, want %d (%s)", label, status, wantStatus, body)
		return
	}
	if wantCode == "" {
		return
	}
	if err := contracttest.Validate(contracttest.Schema(t, "Error"), body); err != nil {
		t.Errorf("%s: body fails the Error schema: %v (%s)", label, err, body)
	}
	var e contract.ErrorResponse
	json.Unmarshal(body, &e)
	if e.Error != wantCode {
		t.Errorf("%s: error code %q, want %q", label, e.Error, wantCode)
	}
}

func TestUploadValidFixtures(t *testing.T) {
	s := newServer(t)
	resultSchema := contracttest.Schema(t, "UploadResult")
	for _, name := range contracttest.Fixtures(t, "valid_") {
		body := contracttest.Fixture(t, name)
		status, res := s.do("POST", "/api/v1/collections", s.token, body)
		if status != http.StatusCreated {
			t.Fatalf("%s: status %d, want 201 (%s)", name, status, res)
		}
		if err := contracttest.Validate(resultSchema, res); err != nil {
			t.Errorf("%s: response fails UploadResult schema: %v", name, err)
		}
		var r contract.UploadResult
		json.Unmarshal(res, &r)
		if r.Status != contract.StatusStored {
			t.Errorf("%s: status %q, want stored", name, r.Status)
		}
		status, res = s.do("POST", "/api/v1/collections", s.token, body)
		json.Unmarshal(res, &r)
		if status != http.StatusOK || r.Status != contract.StatusDuplicate {
			t.Errorf("%s: re-upload = %d %q, want 200 duplicate", name, status, r.Status)
		}
	}
}

func TestUploadRejections(t *testing.T) {
	s := newServer(t)
	minimal := contracttest.Fixture(t, "valid_minimal.json")

	status, body := s.do("POST", "/api/v1/collections", "", minimal)
	expectError(t, "no token", status, body, http.StatusUnauthorized, contract.CodeInvalidToken)
	status, body = s.do("POST", "/api/v1/collections", "unknown-token", minimal)
	expectError(t, "unknown token", status, body, http.StatusUnauthorized, contract.CodeInvalidToken)

	status, body = s.do("POST", "/api/v1/collections", s.token, contracttest.Fixture(t, "invalid_mac_format.json"))
	expectError(t, "schema-invalid", status, body, http.StatusBadRequest, contract.CodeValidation)
	status, body = s.do("POST", "/api/v1/collections", s.token, contracttest.Fixture(t, "invalid_public_subnet.json"))
	expectError(t, "public subnet", status, body, http.StatusBadRequest, contract.CodeValidation)
	wide := contracttest.Modify(t, "valid_minimal.json", func(m map[string]any) {
		m["subnets"].([]any)[0].(map[string]any)["cidr"] = "192.168.0.0/15"
	})
	status, body = s.do("POST", "/api/v1/collections", s.token, wide)
	expectError(t, "wider than /16", status, body, http.StatusBadRequest, contract.CodeValidation)
	status, body = s.do("POST", "/api/v1/collections", s.token, contracttest.Fixture(t, "invalid_schema_version_2.json"))
	expectError(t, "schema_version 2", status, body, http.StatusUnprocessableEntity, contract.CodeUnsupportedSchema)

	huge := append(append([]byte{}, minimal...), bytes.Repeat([]byte(" "), contract.MaxBodyBytes)...)
	status, body = s.do("POST", "/api/v1/collections", s.token, huge)
	expectError(t, "body > 2 MiB", status, body, http.StatusRequestEntityTooLarge, "")

	if err := auth.RevokeCollector(context.Background(), s.app.Store.DB(), s.id, time.Now()); err != nil {
		t.Fatal(err)
	}
	status, body = s.do("POST", "/api/v1/collections", s.token, minimal)
	expectError(t, "revoked token", status, body, http.StatusUnauthorized, contract.CodeInvalidToken)
}

func TestPing(t *testing.T) {
	s := newServer(t)
	status, body := s.do("GET", "/api/v1/ping", s.token, nil)
	if status != http.StatusOK {
		t.Fatalf("ping = %d (%s)", status, body)
	}
	var raw map[string]any
	json.Unmarshal(body, &raw)
	for _, k := range []string{"collector", "server_time", "supported_schema_versions", "ignored_subnets"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("ping response lacks %q: %s", k, body)
		}
	}
	var p contract.PingResponse
	json.Unmarshal(body, &p)
	if p.Collector != "desktop" || len(p.SupportedSchemaVersions) != 1 || p.SupportedSchemaVersions[0] != 1 {
		t.Errorf("ping = %+v", p)
	}
	if !strings.Contains(string(body), `"ignored_subnets":[]`) {
		t.Errorf("ignored_subnets must be an empty array, got %s", body)
	}
	status, body = s.do("GET", "/api/v1/ping", "", nil)
	expectError(t, "ping without token", status, body, http.StatusUnauthorized, contract.CodeInvalidToken)
}
