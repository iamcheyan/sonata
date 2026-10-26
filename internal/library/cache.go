package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CacheVersion is bumped whenever the on-disk index format changes.
const CacheVersion = 1

// LoadCache reads an index from disk. A cache written by a newer version is
// rejected (ErrStaleCache) rather than partially understood.
func LoadCache(path string) (*Library, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lib Library
	if err := json.Unmarshal(data, &lib); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if lib.Version != CacheVersion {
		return nil, ErrStaleCache
	}
	lib.index()
	return &lib, nil
}

// SaveCache writes the index atomically (tmp file + fsync + rename).
func SaveCache(path string, lib *Library) error {
	if lib == nil {
		return errors.New("nil library")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

// Fresh reports whether a cached index still matches the directory state.
func Fresh(lib *Library, root string) bool {
	if lib == nil || filepath.Clean(lib.Root) != filepath.Clean(root) {
		return false
	}
	fp, err := fingerprint(root)
	if err != nil {
		return false
	}
	return fp == lib.Fingerprint
}

func fingerprint(root string) (Fingerprint, error) {
	var fp Fingerprint
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && len(d.Name()) > 0 && d.Name()[0] == '.' {
				return filepath.SkipDir
			}
			return nil
		}
		if !audioExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		fp.Files++
		fp.Bytes += info.Size()
		if m := info.ModTime().Unix(); m > fp.NewestMtime {
			fp.NewestMtime = m
		}
		return nil
	})
	return fp, err
}

// writeAtomic writes data to path via a temporary file in the same directory.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// BackoffUntil is used by callers that want to avoid rescanning a directory
// that keeps failing; kept here so the rule lives next to the scanner.
func BackoffUntil(attempts int, now time.Time) time.Time {
	switch {
	case attempts <= 0:
		return now
	case attempts == 1:
		return now.Add(30 * time.Second)
	case attempts == 2:
		return now.Add(2 * time.Minute)
	default:
		return now.Add(10 * time.Minute)
	}
}
