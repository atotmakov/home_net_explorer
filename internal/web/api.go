package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/atotmakov/home_net_explorer/internal/auth"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// The collector API (contracts/collector-upload-api.yaml). Collectors authenticate with a
// bearer token; owner sessions and the cross-origin check don't apply under /api/v1/.

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, contract.ErrorResponse{Error: code, Detail: detail})
}

func (s *Server) bearerCollector(w http.ResponseWriter, r *http.Request) (store.Collector, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	c, err := auth.Authenticate(r.Context(), s.opts.Store.DB(), strings.TrimSpace(token))
	if !ok || errors.Is(err, auth.ErrInvalidToken) {
		writeAPIError(w, http.StatusUnauthorized, contract.CodeInvalidToken, "missing, unknown or revoked collector token")
		return c, false
	}
	if err != nil {
		s.opts.Log.Error("authenticate collector", "err", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "")
		return c, false
	}
	return c, true
}

// handleUpload: POST /api/v1/collections (FR-008, FR-011).
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	c, ok := s.bearerCollector(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, contract.MaxBodyBytes))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeAPIError(w, http.StatusRequestEntityTooLarge, contract.CodePayloadTooLarge, "the body is larger than 2 MiB")
		return
	case err != nil:
		writeAPIError(w, http.StatusBadRequest, contract.CodeValidation, "could not read the body")
		return
	}
	run, err := contract.Decode(bytes.NewReader(body))
	if err == nil {
		err = contract.Validate(run)
	}
	var ce *contract.Error
	if errors.As(err, &ce) {
		status := http.StatusBadRequest
		if ce.Code == contract.CodeUnsupportedSchema {
			status = http.StatusUnprocessableEntity
		}
		writeAPIError(w, status, ce.Code, ce.Detail)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, contract.CodeValidation, err.Error())
		return
	}

	res, err := s.opts.Ingester.Ingest(r.Context(), c.ID, body, run, s.opts.Clock.Now())
	if err != nil {
		s.opts.Log.Error("ingest", "collector", c.Name, "collection_id", run.CollectionID, "err", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	status := http.StatusCreated
	if res.Status == contract.StatusDuplicate {
		status = http.StatusOK
	}
	s.opts.Log.Info("upload", "collector", c.Name, "collection_id", run.CollectionID, "status", res.Status,
		"observations", len(run.Observations), "new_subnets", len(res.NewSubnets), "clock_skew_ms", res.ClockSkewMs)
	writeJSON(w, status, res)
}

// handlePing: GET /api/v1/ping, which also tells collectors which subnets to skip.
func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	c, ok := s.bearerCollector(w, r)
	if !ok {
		return
	}
	ignored, err := s.opts.Store.IgnoredSubnets(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	resp := contract.PingResponse{
		Collector:               c.Name,
		ServerTime:              s.opts.Clock.Now(),
		SupportedSchemaVersions: []int{contract.SchemaVersion},
		IgnoredSubnets:          []string{},
	}
	for _, p := range ignored {
		resp.IgnoredSubnets = append(resp.IgnoredSubnets, p.String())
	}
	writeJSON(w, http.StatusOK, resp)
}
