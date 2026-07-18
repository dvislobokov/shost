// Package swaggerui serves a fully bundled Swagger UI as a plain
// http.Handler — no middleware, no runtime dependencies, no CDN: the UI
// assets (swagger-ui-dist) are embedded into the binary and work offline.
//
// Mount it on any mux next to your API:
//
//	mux := http.NewServeMux()
//	swaggerui.Mount(mux, "/swagger/", swaggerui.WithSpec("openapi.json", specBytes))
//
// or wire the handler yourself:
//
//	h := swaggerui.Handler(swaggerui.WithSpecURL("/api/openapi.json"))
//	mux.Handle("/swagger/", http.StripPrefix("/swagger", h))
//
// The handler is relative to its mount point: every asset and spec is
// resolved with relative URLs, so any prefix (or none) works unchanged.
package swaggerui

import (
	"embed"
	"encoding/json"
	"html/template"
	"net/http"
	"path"
	"strings"
)

//go:embed assets
var assets embed.FS

// Option customizes the handler.
type Option func(*config)

type specEntry struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type config struct {
	title   string
	entries []specEntry       // what the UI offers to load
	specs   map[string][]byte // documents served by the handler itself
}

// WithSpec serves the given OpenAPI document at ./name relative to the
// mount point and loads it in the UI. name's extension picks the content
// type (.json or .yaml/.yml). May be given multiple times — the UI then
// shows a selector; the first spec is loaded initially.
func WithSpec(name string, spec []byte) Option {
	return func(c *config) {
		name = strings.TrimPrefix(name, "/")
		c.entries = append(c.entries, specEntry{Name: name, URL: "./" + name})
		c.specs[name] = spec
	}
}

// WithSpecURL points the UI at an OpenAPI document served elsewhere (an
// absolute or site-relative URL). May be combined with WithSpec and given
// multiple times.
func WithSpecURL(url string) Option {
	return func(c *config) {
		c.entries = append(c.entries, specEntry{Name: url, URL: url})
	}
}

// WithTitle sets the HTML page title (default "Swagger UI").
func WithTitle(title string) Option {
	return func(c *config) { c.title = title }
}

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="stylesheet" href="./swagger-ui.css">
<style>html{box-sizing:border-box;overflow-y:scroll}*,*:before,*:after{box-sizing:inherit}body{margin:0;background:#fafafa}</style>
</head>
<body>
<div id="swagger-ui"></div>
<script src="./swagger-ui-bundle.js"></script>
<script>
window.ui = SwaggerUIBundle(Object.assign({
  dom_id: "#swagger-ui",
  deepLinking: true,
  presets: [SwaggerUIBundle.presets.apis],
  oauth2RedirectUrl: window.location.href.replace(/[^\/]*(\?.*)?(#.*)?$/, "") + "oauth2-redirect.html"
}, {{.Spec}}));
</script>
</body>
</html>
`))

// Handler returns the Swagger UI as a plain http.Handler. It serves the
// index page at "/" (and "/index.html"), the embedded UI assets, and any
// documents added with WithSpec. Unknown paths return 404.
func Handler(opts ...Option) http.Handler {
	c := &config{title: "Swagger UI", specs: make(map[string][]byte)}
	for _, opt := range opts {
		opt(c)
	}
	index := renderIndex(c)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		switch p {
		case "", ".", "index.html":
			serve(w, "text/html; charset=utf-8", index)
			return
		}
		if spec, ok := c.specs[p]; ok {
			serve(w, specContentType(p), spec)
			return
		}
		if data, err := assets.ReadFile("assets/" + p); err == nil {
			serve(w, assetContentType(p), data)
			return
		}
		http.NotFound(w, r)
	})
}

// Mount registers the handler on mux under prefix (e.g. "/swagger/").
// A request to the prefix without the trailing slash is redirected by
// net/http. Prefix "/" mounts the UI at the root.
func Mount(mux *http.ServeMux, prefix string, opts ...Option) {
	h := Handler(opts...)
	p := strings.TrimSuffix(prefix, "/")
	if p == "" {
		mux.Handle("/", h)
		return
	}
	mux.Handle(p+"/", http.StripPrefix(p, h))
}

func renderIndex(c *config) []byte {
	// The UI's spec source: a single url, or a urls selector when several
	// documents are configured. Without any spec the UI starts empty.
	spec := map[string]any{}
	switch len(c.entries) {
	case 0:
	case 1:
		spec["url"] = c.entries[0].URL
	default:
		spec["urls"] = c.entries
	}
	specJSON, _ := json.Marshal(spec)

	var b strings.Builder
	err := indexTmpl.Execute(&b, struct {
		Title string
		Spec  template.JS
	}{Title: c.title, Spec: template.JS(specJSON)})
	if err != nil {
		panic("swaggerui: render index: " + err.Error())
	}
	return []byte(b.String())
}

func serve(w http.ResponseWriter, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}

func specContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".yaml", ".yml":
		return "application/yaml"
	default:
		return "application/json"
	}
}

func assetContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}
