# app/

The Shogun 2 Save Sync application: a Go backend (see `internal/`) with a
Wails v2 webview UI (`frontend/`). See the [top-level README](../README.md)
for what this does and how to build/install it.

## Local development

```bash
npm install --prefix frontend
wails dev -tags webkit2_41
```

`wails dev` gives hot-reload on the frontend and a devtools-accessible dev
server at http://localhost:34115.

## Testing

```bash
go vet ./...
go test ./...
```
