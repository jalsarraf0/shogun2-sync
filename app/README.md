# app/

The Shogun 2 Save Sync application: a Go backend (see `internal/`) with a
Wails v2 webview UI (`frontend/`). See the [top-level README](../README.md)
for what this does and how to build/install it.

## Local development

Use Go 1.25.0+, Node 22.23.2, npm 10.9.8, Wails 2.13.0, and the platform
dependencies listed in the
[top-level build instructions](../README.md#building-from-source).

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
npm ci --prefix frontend
wails dev -tags webkit2_41
```

`wails dev` gives hot-reload on the frontend and a devtools-accessible dev
server at http://localhost:34115.

## Testing

From a clean checkout, build the frontend before running any Go command that
compiles the main package. `frontend/dist` is intentionally not committed, but
`main.go` embeds it, so `go vet ./...` and `go test ./...` cannot run until it
exists.

```bash
npm ci --prefix frontend
npm test --prefix frontend
npm run build --prefix frontend
go vet ./...
go test ./...
```

CI also runs `gofmt -l .`, `go test -race ./...` on Linux, and the vet/test
suite on Windows. Linux tests need the GTK 3 and WebKit2GTK 4.1 development
packages even when no window is opened.
