package servers

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// apiPrefix is where the API is served when the server also serves the web
// app. The web app calls the API under it, so API routes such as /api-keys
// never clash with the app's pages of the same name.
const apiPrefix = "/api"

// webCSP allows everything from this origin plus Google Fonts. Vue sets
// :style bindings through the CSSOM, which style-src does not block.
const webCSP = "default-src 'self'; script-src 'self'; style-src 'self' https://fonts.googleapis.com; " +
	"font-src https://fonts.gstatic.com; img-src 'self' data:; connect-src 'self'; object-src 'none'; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// Handler returns what the server serves. Without a web directory it is the
// API router at the root. With one, the API moves under /api (plus
// /healthz at the root, for probes) and every other path serves the web
// app.
func (s *Server) Handler() (http.Handler, error) {
	if s.config.WebDir == "" {
		return s.router, nil
	}
	web, err := newWebApp(s.config.WebDir)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle(apiPrefix+"/", http.StripPrefix(apiPrefix, s.router))
	mux.Handle(healthPath, s.router)
	mux.Handle("/", web)
	return mux, nil
}

// webApp serves a built single-page app: files that exist are served as
// they are, and any other path gets index.html so the app's router can
// handle it.
type webApp struct {
	root  *os.Root
	index []byte
}

func newWebApp(dir string) (*webApp, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	index, err := fs.ReadFile(root.FS(), "index.html")
	if err != nil {
		return nil, errors.Join(errors.New("web directory has no index.html"), err)
	}
	return &webApp{root: root, index: index}, nil
}

func (w *webApp) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		rw.Header().Set("Allow", "GET, HEAD")
		http.Error(rw, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	h := rw.Header()
	h.Set("Content-Security-Policy", webCSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	h.Set("Strict-Transport-Security", "max-age=31536000")

	// os.Root refuses paths that leave the directory, so ../ cannot escape.
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name != "" && name != "index.html" {
		if f, err := w.root.Open(name); err == nil {
			defer func() { _ = f.Close() }()
			if info, err := f.Stat(); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					// Vite puts a content hash in every asset's name.
					h.Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					h.Set("Cache-Control", "no-cache")
				}
				http.ServeContent(rw, r, name, info.ModTime(), f)
				return
			}
		}
		// A missing asset is a 404, not the app: a stale page asking for an
		// old hashed file must not get HTML back as JavaScript.
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(rw, r)
			return
		}
	}

	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = rw.Write(w.index)
	}
}
