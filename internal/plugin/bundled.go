package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strings"
)

type BundleRecord struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	AppVersion   string `json:"appVersion"`
	SHA256       string `json:"sha256"`
	SourceCommit string `json:"sourceCommit,omitempty"`
}

var commitID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// LoadBundled opens the same packages an installer would validate. A failed
// package is excluded without preventing the remaining entries from opening.
func LoadBundled(ctx context.Context, assets fs.FS, appVersion string) ([]*Source, []error) {
	index, err := assets.Open("index.json")
	if err != nil {
		return nil, []error{err}
	}
	var records []BundleRecord
	err = decodeJSONReader(index, &records)
	index.Close()
	if err != nil {
		return nil, []error{fmt.Errorf("plugin bundle index: %w", err)}
	}
	var sources []*Source
	var failures []error
	seen := make(map[string]bool)
	for _, record := range records {
		identity := strings.ToLower(record.ID)
		if !validPluginID(record.ID) || seen[identity] || record.AppVersion != appVersion ||
			(record.SourceCommit != "" && !commitID.MatchString(record.SourceCommit)) {
			failures = append(failures, fmt.Errorf("invalid or duplicate plugin bundle receipt"))
			continue
		}
		seen[identity] = true
		source, err := openBundled(ctx, assets, record)
		if err != nil {
			failures = append(failures, fmt.Errorf("bundled plugin %s: %w", record.ID, err))
			continue
		}
		sources = append(sources, source)
	}
	return sources, failures
}

func openBundled(ctx context.Context, assets fs.FS, record BundleRecord) (*Source, error) {
	file, err := assets.Open(record.ID + "/plugin.zip")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var size int64
	input, seekable := file.(io.ReaderAt)
	if seekable {
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}
		size = info.Size()
	} else {
		data, err := io.ReadAll(file)
		if err != nil {
			return nil, err
		}
		size, input = int64(len(data)), bytes.NewReader(data)
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.NewSectionReader(input, 0, size))
	if err != nil {
		return nil, err
	}
	if n != size {
		return nil, io.ErrUnexpectedEOF
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	if fmt.Sprintf("%x", digest) != record.SHA256 {
		return nil, fmt.Errorf("package digest does not match its receipt")
	}
	source, err := openPackage(ctx, input, size, digest, record.AppVersion)
	if err != nil {
		return nil, err
	}
	manifest := source.Manifest()
	if manifest.ID != record.ID || manifest.Version != record.Version {
		return nil, fmt.Errorf("package identity does not match its receipt")
	}
	source.bundled = true
	source.sourceCommit = record.SourceCommit
	return source, nil
}
