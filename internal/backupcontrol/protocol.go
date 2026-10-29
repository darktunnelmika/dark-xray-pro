package backupcontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const archiveHeader = "X-Dark-Pro-Backup-Archive"
const maxMetadataResponse = 8 << 20

func strictDecode(raw []byte, destination any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if walkJSON(d, 0) != nil {
		return ErrInvalid
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(destination) != nil {
		return ErrInvalid
	}
	return nil
}
func walkJSON(d *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrInvalid
	}
	t, e := d.Token()
	if e != nil || t == nil {
		return ErrInvalid
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			v, e := d.Token()
			k, ok := v.(string)
			if e != nil || !ok || seen[k] || len(seen) > MaxReceipts+MaxJobs+32 {
				return ErrInvalid
			}
			seen[k] = true
			if walkJSON(d, depth+1) != nil {
				return ErrInvalid
			}
		}
		v, e := d.Token()
		if e != nil || v != json.Delim('}') {
			return ErrInvalid
		}
	case '[':
		n := 0
		for d.More() {
			n++
			if n > MaxReceipts+MaxJobs+MaxArchives || walkJSON(d, depth+1) != nil {
				return ErrInvalid
			}
		}
		v, e := d.Token()
		if e != nil || v != json.Delim(']') {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func readRequest(w http.ResponseWriter, r *http.Request, dest any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return ErrInvalid
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		return ErrInvalid
	}
	return strictDecode(b, dest)
}
func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func errorCode(err error) (int, error) {
	switch {
	case errors.Is(err, ErrInvalid):
		return 422, ErrInvalid
	case errors.Is(err, ErrConflict):
		return 409, ErrConflict
	case errors.Is(err, ErrNotFound):
		return 404, ErrNotFound
	case errors.Is(err, ErrCapacity):
		return 429, ErrCapacity
	case errors.Is(err, ErrBusy):
		return 409, ErrBusy
	case errors.Is(err, ErrUnsafe):
		return 503, ErrUnsafe
	default:
		return 503, ErrUnavailable
	}
}
func sendError(w http.ResponseWriter, e error) {
	code, public := errorCode(e)
	send(w, code, map[string]string{"error": public.Error()})
}

// NewHandler is only for the Unix listener. It provides no TCP authentication;
// callers must use ServeUnix, which checks the kernel-reported peer UID.
func NewHandler(store *Store, options HandlerOptions) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		value, e := store.Status(r.Context())
		if e != nil {
			sendError(w, e)
			return
		}
		value.Capabilities.Upload = options.Uploads != nil
		if options.RunnerStatus != nil {
			switch v := options.RunnerStatus(); v {
			case "ready", "busy", "recovery_required", "unavailable":
				// A ready execution loop must not hide a queued job or a
				// durable recovery duty observed in the journal.
				if v == "unavailable" || v == "recovery_required" || value.RunnerStatus == "ready" || v == "busy" && value.RunnerStatus != "recovery_required" {
					value.RunnerStatus = v
				}
			default:
				sendError(w, ErrUnavailable)
				return
			}
		}
		send(w, 200, value)
	})
	mux.HandleFunc("POST /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		var q CreateRequest
		if e := readRequest(w, r, &q); e != nil {
			sendError(w, e)
			return
		}
		v, e := store.CreateJob(r.Context(), q)
		if e != nil {
			sendError(w, e)
			return
		}
		send(w, 202, v)
	})
	mux.HandleFunc("GET /v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := store.GetJob(r.Context(), r.PathValue("id"))
		if e != nil {
			sendError(w, e)
			return
		}
		send(w, 200, v)
	})
	mux.HandleFunc("POST /v1/jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		var q CancelRequest
		if e := readRequest(w, r, &q); e != nil {
			sendError(w, e)
			return
		}
		v, e := store.CancelJob(r.Context(), r.PathValue("id"), q)
		if e != nil {
			sendError(w, e)
			return
		}
		send(w, 200, v)
	})
	mux.HandleFunc("PUT /v1/settings", func(w http.ResponseWriter, r *http.Request) {
		var q SettingsRequest
		if e := readRequest(w, r, &q); e != nil {
			sendError(w, e)
			return
		}
		v, e := store.UpdateSettings(r.Context(), q)
		if e != nil {
			sendError(w, e)
			return
		}
		send(w, 200, v)
	})
	mux.HandleFunc("GET /v1/archives/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		f, a, e := store.OpenDownload(r.Context(), r.PathValue("id"), options.OpenArchive)
		if e != nil {
			sendError(w, e)
			return
		}
		defer f.Close()
		raw, _ := json.Marshal(a)
		w.Header().Set(archiveHeader, base64.RawURLEncoding.EncodeToString(raw))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="dark-pro-backup-`+a.ID+`.backup"`)
		w.Header().Set("ETag", `"`+a.SHA256+`"`)
		controller := http.NewResponseController(w)
		if controller.SetWriteDeadline(time.Time{}) != nil {
			return
		}
		transfer := &downloadResponseWriter{ResponseWriter: w}
		http.ServeContent(transfer, r, "dark-pro-backup-"+a.ID+".backup", time.Unix(a.CreatedUnix, 0), f)
		if transfer.err != nil || transfer.written != a.SizeBytes || controller.Flush() != nil {
			return
		}
		// Completing the upstream write does not prove that HUB has sent its
		// final buffered bytes to the owner. Typed Download keeps a dedicated
		// connection open until its Body.Close. Keep this handler active and
		// pinned until that close (or process death), with no timed lease expiry.
		<-r.Context().Done()
	})
	attachUploadHandlers(mux, store, options.Uploads)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if store == nil {
			sendError(w, ErrUnavailable)
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			sendError(w, ErrInvalid)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

type downloadResponseWriter struct {
	http.ResponseWriter
	written int64
	err     error
}

func (w *downloadResponseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.written += int64(n)
	if err != nil {
		w.err = err
	}
	return n, err
}

func (w *downloadResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func decodeError(response *http.Response) error {
	defer response.Body.Close()
	b, e := io.ReadAll(io.LimitReader(response.Body, 4097))
	if e != nil || len(b) > 4096 {
		return ErrUnavailable
	}
	var v struct {
		Error string `json:"error"`
	}
	if strictDecode(b, &v) != nil {
		return ErrUnavailable
	}
	for _, known := range []error{ErrInvalid, ErrConflict, ErrNotFound, ErrCapacity, ErrBusy, ErrUnsafe, ErrUnavailable} {
		if v.Error == known.Error() {
			return known
		}
	}
	return ErrUnavailable
}

// Client bounds metadata calls to eight seconds. A download has a dedicated
// Unix connection governed by the caller's context and explicitly closed when
// its downstream consumer is done, including after all response bytes arrived.
type Client struct {
	metadata     *http.Client
	dialDownload func(context.Context) (net.Conn, error)
}

func (c *Client) call(ctx context.Context, method, path string, input, dest any) error {
	var body io.Reader
	if input != nil {
		raw, e := json.Marshal(input)
		if e != nil || len(raw) > 4096 {
			return ErrInvalid
		}
		body = bytes.NewReader(raw)
	}
	req, e := http.NewRequestWithContext(ctx, method, "http://backup-control"+ProtocolBase+path, body)
	if e != nil {
		return ErrInvalid
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, e := c.metadata.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		return decodeError(resp)
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, maxMetadataResponse+1))
	if e != nil || len(raw) > maxMetadataResponse || strictDecode(raw, dest) != nil {
		return ErrUnavailable
	}
	return nil
}
func (c *Client) Status(ctx context.Context) (Status, error) {
	var v Status
	e := c.call(ctx, "GET", "/status", nil, &v)
	return v, e
}
func (c *Client) CreateJob(ctx context.Context, r CreateRequest) (JobResponse, error) {
	var v JobResponse
	e := c.call(ctx, "POST", "/jobs", r, &v)
	return v, e
}
func (c *Client) GetJob(ctx context.Context, id string) (Job, error) {
	var v Job
	if !ValidID(id) {
		return v, ErrInvalid
	}
	e := c.call(ctx, "GET", "/jobs/"+id, nil, &v)
	return v, e
}
func (c *Client) CancelJob(ctx context.Context, id string, r CancelRequest) (JobResponse, error) {
	var v JobResponse
	if !ValidID(id) {
		return v, ErrInvalid
	}
	e := c.call(ctx, "POST", "/jobs/"+id+"/cancel", r, &v)
	return v, e
}
func (c *Client) UpdateSettings(ctx context.Context, r SettingsRequest) (SettingsResponse, error) {
	var v SettingsResponse
	e := c.call(ctx, "PUT", "/settings", r, &v)
	return v, e
}
func (c *Client) Download(ctx context.Context, id string) (*ArchiveDownload, error) {
	if !ValidID(id) {
		return nil, ErrInvalid
	}
	req, e := http.NewRequestWithContext(ctx, "GET", "http://backup-control"+ProtocolBase+"/archives/"+id+"/download", nil)
	if e != nil {
		return nil, ErrInvalid
	}
	conn, e := c.dialDownload(ctx)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	connection := &downloadConnection{Conn: conn, done: make(chan struct{})}
	retained := false
	defer func() {
		if !retained {
			_ = connection.Close()
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-connection.done:
		}
	}()
	if e = req.Write(conn); e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	resp, e := readDownloadResponse(bufio.NewReaderSize(conn, 16<<10), req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	resp.Body = &downloadBody{ReadCloser: resp.Body, connection: connection}
	if resp.StatusCode != 200 {
		return nil, decodeError(resp)
	}
	raw, e := base64.RawURLEncoding.DecodeString(resp.Header.Get(archiveHeader))
	var a Archive
	if e != nil || len(raw) > 4096 || strictDecode(raw, &a) != nil || validateArchive(a) != nil || a.ID != id || !a.Downloadable || resp.ContentLength != a.SizeBytes {
		resp.Body.Close()
		return nil, ErrUnsafe
	}
	retained = true
	return &ArchiveDownload{Archive: a, Body: resp.Body, ContentLength: resp.ContentLength}, nil
}

func readDownloadResponse(reader *bufio.Reader, request *http.Request) (*http.Response, error) {
	// bufio's capacity alone does not bound net/http's MIME-header parser.
	// Bound only the header, then retain the same buffered socket for the body.
	header := make([]byte, 0, 1024)
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil || len(header)+len(line) > 16<<10 {
			return nil, ErrUnsafe
		}
		header = append(header, line...)
		if bytes.Equal(line, []byte("\r\n")) || bytes.Equal(line, []byte("\n")) {
			break
		}
	}
	return http.ReadResponse(bufio.NewReader(io.MultiReader(bytes.NewReader(header), reader)), request)
}

type downloadConnection struct {
	net.Conn
	done chan struct{}
	once sync.Once
	err  error
}

func (c *downloadConnection) Close() error {
	c.once.Do(func() { c.err = c.Conn.Close(); close(c.done) })
	return c.err
}

type downloadBody struct {
	io.ReadCloser
	connection *downloadConnection
	once       sync.Once
	err        error
}

func (b *downloadBody) Close() error {
	b.once.Do(func() {
		// Closing the socket first also prevents net/http's response Body
		// from draining an arbitrarily slow remainder on an aborted download.
		_ = b.connection.Close()
		b.err = b.ReadCloser.Close()
	})
	return b.err
}
