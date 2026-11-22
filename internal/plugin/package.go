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

var operations = map[provider.Capability][]string{
	provider.BrowseLibrary:                     {"readAccount"},
	provider.BrowseFollowing:                   {"readFollowing"},
	provider.BrowseCatalog:                     {"browse"},
	provider.GetCatalogItem:                    {"getItem"},
	provider.SearchCatalog:                     {"search"},
	provider.SearchSuggest:                     {"searchSuggest"},
	provider.ReadCreator:                       {"readCreator"},
	provider.FollowCreator:                     {"followCreator"},
	provider.EditVideo:                         {"editVideo"},
	provider.ReadVideoComments:                 {"videoComments"},
	provider.WriteVideoComments:                {"editVideo"},
	provider.ResolvePlayback:                   {"resolvePlayback"},
	provider.ReportPlayback:                    {"reportPlayback"},
	provider.ReadSubtitles:                     {"readSubtitles"},
	provider.ReadDanmaku:                       {"readDanmaku"},
	provider.WriteDanmaku:                      {"postDanmaku"},
	provider.EditVideoFavorites:                {"videoFavoriteFolders", "editVideoFavoriteFolders"},
	provider.SignIn:                            {beginSignIn, pollSignIn, restoreAccount, signOut},
	provider.AccountHistory.Capability():       {"readAccount"},
	provider.AccountWatchLater.Capability():    {"readAccount"},
	provider.AccountFavorites.Capability():     {"readAccount"},
	provider.AccountTracking.Capability():      {"readAccount"},
	provider.AccountCourses.Capability():       {"readAccount"},
	provider.AccountNotifications.Capability(): {"readAccount"},
	provider.EditFavoriteFolders:               {"editFavoriteFolder"},
	provider.EditHistory:                       {"removeAccountItems"},
	provider.EditWatchLater:                    {"removeAccountItems"},
	provider.EditTracking:                      {"removeAccountItems"},
}

type Source struct {
	manifest    sourceManifest
	cover       []byte
	authArtwork []byte
	module      *moejs.Module
	digest      [sha256.Size]byte
}

func (s *Source) Manifest() Manifest        { return s.manifest.Manifest }
func (s *Source) Cover() []byte             { return s.cover }
func (s *Source) AuthArtwork() []byte       { return s.authArtwork }
func (s *Source) Digest() [sha256.Size]byte { return s.digest }
func (s *Source) Capabilities() provider.Capabilities {
	return append(provider.Capabilities(nil), s.manifest.Capabilities...)
}

func (s *Source) AuthPresentation() provider.AuthPresentation {
	return s.manifest.Authentication.Presentation
}

func (s *Source) Presentation() provider.SourcePresentation {
	return s.manifest.Presentation
}

// Open validates and compiles a package without executing its code.
func Open(ctx context.Context, data []byte, appVersion string) (*Source, error) {
	return openPackage(ctx, bytes.NewReader(data), int64(len(data)), sha256.Sum256(data), appVersion)
}

func openPackage(ctx context.Context, input io.ReaderAt, size int64, digest [sha256.Size]byte, appVersion string) (*Source, error) {
	files, err := readArchive(input, size)
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
		names, ok := operations[capability]
		if !ok || seen[capability] {
			return nil, fmt.Errorf("%w: invalid operation declaration %q", ErrIncompatible, capability)
		}
		for _, name := range names {
			if !exports[name] {
				return nil, fmt.Errorf("%w: missing operation %q", ErrIncompatible, name)
			}
		}
		seen[capability] = true
	}
	cover, err := artworkFile(files, manifest.Artwork.Cover)
	if err != nil {
		return nil, err
	}
	authArtwork := cover
	if name := manifest.Authentication.Presentation.GuestArtwork; name != manifest.Artwork.Cover {
		authArtwork, err = artworkFile(files, name)
		if err != nil {
			return nil, err
		}
	}
	return &Source{manifest: manifest, cover: cover, authArtwork: authArtwork, module: module, digest: digest}, nil
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

func readArchive(input io.ReaderAt, size int64) (*packageArchive, error) {
	reader, err := zip.NewReader(input, size)
	if err != nil {
		return nil, fmt.Errorf("plugin archive: %w", err)
	}
	// Expand only manifest, imported code and cover files. Use the same memory
	// allowance as source execution when reading their uncompressed bytes.
	archive := &packageArchive{files: make(map[string]*zip.File), remaining: heapLimit}
	paths := make(map[string]bool)
	for _, file := range reader.File {
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

func artworkFile(files *packageArchive, name string) ([]byte, error) {
	if name == "" {
		return nil, nil
	}
	if !packagePath(name) {
		return nil, fmt.Errorf("invalid artwork path %q", name)
	}
	format, ok := map[string]string{".png": "png", ".jpg": "jpeg", ".jpeg": "jpeg", ".webp": "webp"}[strings.ToLower(path.Ext(name))]
	if !ok {
		return nil, fmt.Errorf("unsupported artwork extension")
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
