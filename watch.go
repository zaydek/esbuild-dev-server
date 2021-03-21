package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	p "path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

// parseAppFromCLIOptions parses a Retro app from command-line options.
func newApp() *RetroApp {
	app := &RetroApp{
		WatchPoll:      100 * time.Millisecond,
		WatchDirectory: "src",
		BuildDirectory: "public",
		ServePort:      8000,
	}
	return app
}

type RetroApp struct {
	esbuildResult   api.BuildResult
	esbuildWarnings []api.Message
	esbuildErrors   []api.Message

	WatchPoll      time.Duration
	WatchDirectory string
	BuildDirectory string
	ServePort      int
}

func (a *RetroApp) WarningString() string {
	msg := a.esbuildWarnings[0]
	cwd, _ := os.Getwd()

	gap := len(strconv.Itoa(msg.Location.Line))
	return fmt.Sprintf("vscode://file%s/%s:%d:%d: %s", cwd, msg.Location.File, msg.Location.Line, msg.Location.Column, msg.Text) + `

    ` + fmt.Sprintf("// ./%s", msg.Location.File) + `
    ` + fmt.Sprintf("%-*d | %s", gap, msg.Location.Line+0, msg.Location.LineText) + `
    ` + fmt.Sprintf("%-*d | %s^", gap, msg.Location.Line+1, strings.Repeat(" ", msg.Location.Column)) + `
    ` + fmt.Sprintf("%-*d | %s%s", gap, msg.Location.Line+2, strings.Repeat(" ", msg.Location.Column), msg.Text) + `
`
}

func (a *RetroApp) ErrorString() string {
	msg := a.esbuildErrors[0]
	// cwd, _ := os.Getwd()

	gap := len(strconv.Itoa(msg.Location.Line))
	return fmt.Sprintf("%s:%d:%d: %s", msg.Location.File, msg.Location.Line, msg.Location.Column, msg.Text) + `

    ` + fmt.Sprintf("// ./%s", msg.Location.File) + `
    ` + fmt.Sprintf("%-*d | %s", gap, msg.Location.Line+0, msg.Location.LineText) + `
    ` + fmt.Sprintf("%-*d | %s^", gap, msg.Location.Line+1, strings.Repeat(" ", msg.Location.Column)) + `
    ` + fmt.Sprintf("%-*d | %s%s", gap, msg.Location.Line+2, strings.Repeat(" ", msg.Location.Column), msg.Text) + `
`
}

func (a *RetroApp) HTMLErrorString() string {
	msg := a.esbuildErrors[0]
	cwd, _ := os.Getwd()

	return `<!DOCTYPE html>
<html>
	<head>
		<title>
			` + fmt.Sprintf("Error: %s", msg.Text) + `
		</title>
		<style>

a {
	color: unset;
	text-decoration: unset;
}

body {
	color: hsla(0, 0%, 0%, 0.95);
	background-color: #fff;
}

@media (prefers-color-scheme: dark) {
	body {
		color: hsla(0, 0%, 100%, 0.95);
		background-color: rgb(32, 33, 36);
	}
}

		</style>
	</head>
	<body>
		<a href="` + fmt.Sprintf("vscode://file%s/%s:%d:%d", cwd, msg.Location.File, msg.Location.Line, msg.Location.Column+1) + `">
			<pre><code>` + a.ErrorString() + `</code></pre>
		</a>
		<script type="module">const dev = new EventSource("/~dev"); dev.addEventListener("reload", () => { localStorage.setItem("/~dev", "" + Date.now()); window.location.reload() }); dev.addEventListener("error", e => { try { console.error(JSON.parse(e.data)) } catch {} }); window.addEventListener("storage", e => { if (e.key === "/~dev") { window.location.reload() } })</script>
	</body>
</html>
`
}

func (a *RetroApp) Build() {
	results := api.Build(api.BuildOptions{
		Bundle:      true,
		EntryPoints: []string{path.Join(a.WatchDirectory, "index.js")},
		Incremental: true,
		Outfile:     path.Join(a.BuildDirectory, "app.js"),
		Write:       true,
	})
	a.esbuildResult = results
	a.esbuildWarnings = results.Warnings
	a.esbuildErrors = results.Errors
}

func (a *RetroApp) Rebuild() {
	// start := time.Now()

	results := a.esbuildResult.Rebuild()
	a.esbuildResult = results
	a.esbuildWarnings = results.Warnings
	a.esbuildErrors = results.Errors

	// stdout.Printf("⚡️ %0.3fs\n", time.Since(start).Seconds())
}

func newWatcher(dir string, poll time.Duration) <-chan struct{} {
	var (
		change = make(chan struct{})
		modMap = map[string]time.Time{}
	)

	go func() {
		defer close(change)
		for range time.Tick(poll) {
			if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				if prev, ok := modMap[path]; !ok {
					modMap[path] = info.ModTime()
				} else {
					if next := info.ModTime(); prev != next {
						modMap[path] = next
						change <- struct{}{}
					}
				}
				return nil
			}); err != nil {
				stderr.Println(err)
				os.Exit(1)
			}
		}
	}()
	return change
}

var (
	stdout = NewLogger(os.Stdout)
	stderr = NewLogger(os.Stderr)
)

func main() {
	var (
		app     = newApp()
		browser = make(chan ServerSentEvent, 1)
	)

	app.Build()

	stdout.Printf("👾 http://localhost:%d\n", app.ServePort)

	if len(app.esbuildWarnings) > 0 {
		stderr.Println(app.WarningString())
		// (Do not return or os.Exit(1))
	}
	if len(app.esbuildErrors) > 0 {
		stderr.Println(app.ErrorString())
		// (Do not return or os.Exit(1))
	}

	go func() {
		for range newWatcher(app.WatchDirectory, app.WatchPoll) {
			app.Rebuild()
			browser <- ServerSentEvent{Event: "reload"}
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if ext := path.Ext(r.URL.Path); ext == "" {
			app.Rebuild()
			if len(app.esbuildWarnings) > 0 {
				stderr.Println(app.WarningString())
				data, _ := json.Marshal(app.WarningString())
				defer func() {
					// Pause 100ms so server-sent events do not drop on refresh
					time.Sleep(100 * time.Millisecond)
					browser <- ServerSentEvent{Event: "warning", Data: string(data)}
				}()
			}
			if len(app.esbuildErrors) > 0 {
				stderr.Println(app.ErrorString())
				fmt.Fprintln(w, app.HTMLErrorString())
				return
			}
		}
		http.ServeFile(w, r, p.Join(string(app.BuildDirectory), r.URL.Path))
	})

	http.HandleFunc("/~dev", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, _ := w.(http.Flusher)
		// if !ok {
		// 	stderr.Println("Your browser doesn’t appear to support server-sent events (SSE). " +
		// 		"This means changes to your source code can’t automatically refresh your browser tab.")
		// 	return
		// }
		for {
			select {
			case e := <-browser:
				e.Write(w)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	if err := http.ListenAndServe(fmt.Sprintf(":%d", app.ServePort), nil); err != nil {
		stderr.Println(err)
		os.Exit(1)
	}
}
