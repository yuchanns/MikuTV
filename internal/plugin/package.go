package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"path"
	"strings"
	"unicode"

	"github.com/MoeForge/moejs"
	"github.com/yuchanns/MikuTV/internal/provider"
	_ "golang.org/x/image/webp"
)

var operations = map[provider.Capability]string{
	provider.BrowseCatalog:  "browse",
	provider.GetCatalogItem: "getItem",
}

type Source struct {
	manifest sourceManifest
	cover    []byte
	module   *moejs.Module
	digest   [sha256.Size]byte
}

func (s *Source) Manifest() Manifest        { return s.manifest.Manifest }
func (s *Source) Cover() []byte             { return bytes.Clone(s.cover) }
func (s *Source) Digest() [sha256.Size]byte { return s.digest }
func (s *Source) Capabilities() provider.Capabilities {
	return append(provider.Capabilities(nil), s.manifest.Capabilities...)
}

// Open validates and compiles a package without executing its code.
func Open(ctx context.Context, data []byte, appVersion string) (*Source, error) {
	return openPackage(ctx, bytes.NewReader(data), int64(len(data)), sha256.Sum256(data), appVersion)
}

func openPackage(ctx context.Context, input io.ReaderAt, size int64, digest [sha256.Size]byte, appVersion string) (*Source, error) {
	files, err := readArchive(ctx, input, size)
	if err != nil {
		return nil, err
	}
	header, err := files.read("plugin.json")
	if err != nil {
		return nil, fmt.Errorf("plugin.json: %w", err)
	}
	manifest, err := decodeSource(header, appVersion)
	if err != nil {
		return nil, err
	}
	compileContext, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	module, err := compilePackage(compileContext, files, manifest.Main)
	if err != nil {
		return nil, err
	}
	exports := make(map[string]bool)
	for _, name := range module.Exports() {
		exports[name] = true
	}
	seen := make(map[provider.Capability]bool)
	for _, capability := range manifest.Capabilities {
		name, ok := operations[capability]
		if !ok || !exports[name] || seen[capability] {
			return nil, fmt.Errorf("%w: invalid operation declaration %q", ErrIncompatible, capability)
		}
		seen[capability] = true
	}
	cover, err := coverFile(files, manifest.Artwork.Cover)
	if err != nil {
		return nil, err
	}
	return &Source{manifest: manifest, cover: cover, module: module, digest: digest}, nil
}

func packagePath(name string) bool {
	return name != "." && fs.ValidPath(name) && !strings.ContainsAny(name, `\:`) &&
		strings.IndexFunc(name, unicode.IsControl) == -1
}

func modulePath(name string) bool {
	return packagePath(name) && (path.Ext(name) == ".js" || path.Ext(name) == ".mjs")
}

type packageArchive struct {
	files     map[string]*zip.File
	remaining uint64
}

func readArchive(ctx context.Context, input io.ReaderAt, size int64) (*packageArchive, error) {
	reader, err := zip.NewReader(input, size)
	if err != nil {
		return nil, fmt.Errorf("plugin archive: %w", err)
	}
	// Expand only manifest, imported code and cover files. Use the same memory
	// allowance as source execution when reading their uncompressed bytes.
	archive := &packageArchive{files: make(map[string]*zip.File), remaining: heapLimit}
	paths := make(map[string]bool)
	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(file.Name, "/")
		lower := strings.ToLower(name)
		if !packagePath(name) || file.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("invalid package path %q", file.Name)
		}
		if _, exists := paths[lower]; exists {
			return nil, fmt.Errorf("duplicate package path %q", name)
		}
		paths[lower] = file.FileInfo().IsDir()
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.Mode().IsRegular() {
			return nil, fmt.Errorf("invalid package file %q", name)
		}
		archive.files[name] = file
	}
	for name := range paths {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if dir, exists := paths[parent]; exists && !dir {
				return nil, fmt.Errorf("package path %q is inside a file", name)
			}
		}
	}
	return archive, nil
}

func (a *packageArchive) read(name string) ([]byte, error) {
	file := a.files[name]
	if file == nil {
		return nil, fs.ErrNotExist
	}
	if file.UncompressedSize64 > a.remaining {
		return nil, errors.New("source package memory budget exceeded")
	}
	body, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer body.Close()
	size := int64(file.UncompressedSize64)
	content, err := io.ReadAll(io.LimitReader(body, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) != size {
		return nil, errors.New("invalid uncompressed size")
	}
	a.remaining -= uint64(len(content))
	return content, nil
}

func coverFile(files *packageArchive, name string) ([]byte, error) {
	if name == "" {
		return nil, nil
	}
	if !packagePath(name) {
		return nil, fmt.Errorf("invalid cover path %q", name)
	}
	format, ok := map[string]string{".png": "png", ".jpg": "jpeg", ".jpeg": "jpeg", ".webp": "webp"}[strings.ToLower(path.Ext(name))]
	if !ok {
		return nil, fmt.Errorf("unsupported cover extension")
	}
	data, err := files.read(name)
	if err != nil {
		return nil, nil
	}
	cfg, actual, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || actual != format || cfg.Width <= 0 || cfg.Height <= 0 ||
		cfg.Width > heapLimit/4/cfg.Height {
		return nil, nil
	}
	return data, nil
}

func compilePackage(ctx context.Context, files *packageArchive, entry string) (*moejs.Module, error) {
	modules := make(map[string]*moejs.Module)
	compile := func(name string) (*moejs.Module, error) {
		if m := modules[name]; m != nil {
			return m, nil
		}
		if !modulePath(name) {
			return nil, fmt.Errorf("invalid module path %q", name)
		}
		data, err := files.read(name)
		if err != nil {
			return nil, fmt.Errorf("module %q: %w", name, err)
		}
		m, err := moejs.CompileWith(name, string(data), moejs.CompileOptions{Context: ctx})
		if err == nil {
			modules[name] = m
		}
		return m, err
	}
	main, err := compile(entry)
	if err != nil {
		return nil, err
	}
	return moejs.Link(main, func(referrer moejs.Referrer, specifier string) (*moejs.Module, error) {
		if !strings.HasPrefix(specifier, "./") && !strings.HasPrefix(specifier, "../") {
			return nil, fmt.Errorf("expected a package-relative import")
		}
		return compile(path.Join(path.Dir(referrer.Name()), specifier))
	})
}
