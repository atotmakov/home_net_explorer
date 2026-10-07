package upload

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Spool keeps finished runs on disk until the server has them (FR-010). Each run is written
// to "<collection_id>.json" via a temporary file and a rename, so a crash never leaves a
// half-written run that looks complete.
type Spool struct{ Dir string }

// Item is a pending run.
type Item struct {
	ID      string
	Path    string
	ModTime time.Time
}

const tmpPrefix = ".tmp-"

// Save writes run to the spool and returns its JSON body.
func (s Spool) Save(run *contract.CollectionRun) ([]byte, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, err
	}
	body, err := json.Marshal(run)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(s.Dir, tmpPrefix+"*.json")
	if err != nil {
		return nil, err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	if err := os.Rename(tmp.Name(), s.path(run.CollectionID)); err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	return body, nil
}

func (s Spool) path(id string) string { return filepath.Join(s.Dir, id+".json") }

// Pending lists runs waiting for upload, oldest first. Leftover temporary files from an
// interrupted Save are deleted.
func (s Spool) Pending() ([]Item, error) {
	entries, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		if strings.HasPrefix(name, tmpPrefix) {
			os.Remove(filepath.Join(s.Dir, name))
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, Item{ID: strings.TrimSuffix(name, ".json"), Path: filepath.Join(s.Dir, name), ModTime: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].ModTime.Equal(items[j].ModTime) {
			return items[i].ModTime.Before(items[j].ModTime)
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

// Remove deletes an uploaded run.
func (s Spool) Remove(id string) error {
	err := os.Remove(s.path(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Reject moves a run the server refused (400/413/422) to spool/rejected/, so it is kept for
// inspection but never retried.
func (s Spool) Reject(id string) error {
	dir := filepath.Join(s.Dir, "rejected")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Rename(s.path(id), filepath.Join(dir, id+".json")); err != nil {
		return fmt.Errorf("upload: reject %s: %w", id, err)
	}
	return nil
}
