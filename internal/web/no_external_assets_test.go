package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// Page views must not make the browser fetch anything from a third party
// (CDNs, font hosts, analytics). Plain <a href> links the user clicks are
// fine; anything the browser loads on its own is not.
var (
	externalTagAsset = regexp.MustCompile(`(?is)<(?:script|link|img|iframe|source|audio|video|embed|object)\b[^>]*\b(?:src|href|data)\s*=\s*["']?(?:https?:)?//`)
	externalCSSURL   = regexp.MustCompile(`(?i)(?:@import\s+(?:url\(\s*)?|url\(\s*)["']?(?:https?:)?//`)
	localAssetRef    = regexp.MustCompile(`["'](/fs/public/[^"'?#]+)`)
)

func checkNoExternalAssets(t *testing.T, fsys fs.FS, root string) {
	t.Helper()
	err := fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".html") && !strings.HasSuffix(path, ".css") && !strings.HasSuffix(path, ".js") {
			return nil
		}
		b, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		s := string(b)
		if m := externalTagAsset.FindString(s); m != "" {
			t.Errorf("%s loads a third-party asset: %q", path, m)
		}
		if m := externalCSSURL.FindString(s); m != "" {
			t.Errorf("%s references a third-party URL in CSS: %q", path, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTemplatesLoadNoThirdPartyAssets(t *testing.T) {
	checkNoExternalAssets(t, templFS, "templates")
}

func TestPublicAssetsLoadNoThirdPartyAssets(t *testing.T) {
	checkNoExternalAssets(t, pubFS, "public")
}

// Every /fs/public/... path the templates reference must be embedded,
// otherwise the vendored copy silently 404s.
func TestTemplateLocalAssetsAreEmbedded(t *testing.T) {
	entries, err := fs.ReadDir(templFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := fs.ReadFile(templFS, "templates/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range localAssetRef.FindAllStringSubmatch(string(b), -1) {
			p := strings.TrimPrefix(m[1], "/fs/")
			if _, err := fs.Stat(pubFS, p); err != nil {
				t.Errorf("templates/%s references %s, not embedded: %v", e.Name(), m[1], err)
			}
		}
	}
}

// Fonts referenced by vendored CSS via relative url() must be embedded too.
func TestVendoredCSSRelativeURLsResolve(t *testing.T) {
	rel := regexp.MustCompile(`url\(\s*["']?(\./[^"')?#]+)`)
	for _, css := range []string{
		"public/vendor/bootstrap-icons/bootstrap-icons.css",
		"public/vendor/source-sans-pro/source-sans-pro.css",
	} {
		b, err := fs.ReadFile(pubFS, css)
		if err != nil {
			t.Fatal(err)
		}
		dir := css[:strings.LastIndex(css, "/")]
		for _, m := range rel.FindAllStringSubmatch(string(b), -1) {
			p := dir + "/" + strings.TrimPrefix(m[1], "./")
			if _, err := fs.Stat(pubFS, p); err != nil {
				t.Errorf("%s references %s, not embedded: %v", css, m[1], err)
			}
		}
	}
}
