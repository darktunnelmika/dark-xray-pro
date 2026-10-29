package backupcontrol

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupimport"
)

const uploadHeader = "X-Dark-Pro-Backup-Upload"
const uploadActorHeader = "X-Dark-Pro-Upload-Actor"
const UploadSHA256Header = "X-Backup-SHA256"

// UploadStore is independent of the successful native backup journal. It only
// receives opaque ciphertext; it has no key, schema or activation operation.
type UploadStore interface {
	Status(context.Context) (backupimport.Status, error)
	Create(context.Context, backupimport.CreateRequest) (backupimport.Response, error)
	Get(context.Context, string) (backupimport.Upload, error)
	Receive(context.Context, string, string, int64, io.Reader) (backupimport.Response, error)
	Cancel(context.Context, string, string) (backupimport.Response, error)
	Remove(context.Context, string, string) (backupimport.Response, error)
	OpenDownload(context.Context, string) (backupimport.ReadSeekCloser, backupimport.Upload, error)
}

// UploadResponse retains the worker transfer lease after its small receipt
// has been read. HUB closes it only after flushing the response to the owner.
type UploadResponse struct {
	Response backupimport.Response
	Lease    io.Closer
}

type UploadDownload struct {
	Upload        backupimport.Upload
	Body          io.ReadCloser
	ContentLength int64
}

func importError(err error) error {
	for _, pair := range [][2]error{
		{backupimport.ErrInvalid, ErrInvalid}, {backupimport.ErrConflict, ErrConflict},
		{backupimport.ErrBusy, ErrBusy}, {backupimport.ErrCapacity, ErrCapacity},
		{backupimport.ErrNotFound, ErrNotFound}, {backupimport.ErrUnsafe, ErrUnsafe},
	} {
		if errors.Is(err, pair[0]) {
			return pair[1]
		}
	}
	return ErrUnavailable
}

func disabledUploads() backupimport.Status {
	return backupimport.Status{MaxUploadBytes: backupimport.MaxUploadBytes, MaxInboxBytes: backupimport.MaxInboxBytes,
		MaxEntries: backupimport.MaxEntries, MaxActiveEntries: backupimport.MaxActiveEntries, Uploads: []backupimport.Upload{}}
}

func attachUploadHandlers(mux *http.ServeMux, control *Store, inbox UploadStore) {
	available := func(w http.ResponseWriter) bool {
		if inbox == nil {
			sendError(w, ErrUnavailable)
			return false
		}
		return true
	}
	mux.HandleFunc("GET /v1/uploads", func(w http.ResponseWriter, r *http.Request) {
		if inbox == nil {
			send(w, 200, disabledUploads())
			return
		}
		value, err := inbox.Status(r.Context())
		if err != nil {
			sendError(w, importError(err))
			return
		}
		send(w, 200, value)
	})
	mux.HandleFunc("POST /v1/uploads", func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		var input backupimport.CreateRequest
		if readRequest(w, r, &input) != nil {
			sendError(w, ErrInvalid)
			return
		}
		value, err := inbox.Create(r.Context(), input)
		if err != nil {
			sendError(w, importError(err))
			return
		}
		send(w, 200, value)
	})
	mux.HandleFunc("GET /v1/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		value, err := inbox.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			sendError(w, importError(err))
			return
		}
		send(w, 200, value)
	})
	for _, action := range []string{"cancel", "remove"} {
		mux.HandleFunc("POST /v1/uploads/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			if !available(w) {
				return
			}
			var input struct {
				ActorID string `json:"actor_id"`
			}
			if readRequest(w, r, &input) != nil {
				sendError(w, ErrInvalid)
				return
			}
			var value backupimport.Response
			var err error
			if action == "cancel" {
				value, err = inbox.Cancel(r.Context(), r.PathValue("id"), input.ActorID)
			} else {
				value, err = inbox.Remove(r.Context(), r.PathValue("id"), input.ActorID)
			}
			if err != nil {
				sendError(w, importError(err))
				return
			}
			send(w, 200, value)
		})
	}
	mux.HandleFunc("PUT /v1/uploads/{id}/content", func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		id, actor, hash := r.PathValue("id"), r.Header.Get(uploadActorHeader), r.Header.Get(UploadSHA256Header)
		if r.Header.Get("Content-Type") != "application/octet-stream" || r.Header.Get("Content-Encoding") != "" ||
			len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > backupimport.MaxUploadBytes ||
			!ValidID(id) || !actorPattern.MatchString(actor) || !digestPattern.MatchString(hash) {
			sendError(w, ErrInvalid)
			return
		}
		entry, err := inbox.Get(r.Context(), id)
		if err != nil {
			sendError(w, importError(err))
			return
		}
		if entry.ActorID != actor || entry.SizeBytes != r.ContentLength || entry.SHA256 != hash {
			sendError(w, ErrConflict)
			return
		}
		if entry.State == "received" {
			// Completed receipts use a bodyless dedicated connection below.
			// HTTP transports may close an early-final upload connection before
			// HUB has flushed its downstream receipt, so it cannot hold a pin.
			sendError(w, ErrConflict)
			return
		}
		ctx, body, cleanup, err := PrepareUpload(w, r)
		if err != nil {
			sendError(w, err)
			return
		}
		defer cleanup()
		release, err := control.AcquireTransfer(ctx)
		if err != nil {
			sendError(w, err)
			return
		}
		defer release()
		value, err := inbox.Receive(ctx, id, actor, r.ContentLength, body)
		if err != nil {
			sendError(w, importError(err))
			return
		}
		if value.Replayed {
			// Another transfer completed between the lookup and Receive. The
			// caller reconciles the same ID; no body or artifact is replaced.
			sendError(w, ErrConflict)
			return
		}
		raw, err := json.Marshal(value)
		if err != nil || len(raw) > 4096 {
			sendError(w, ErrUnsafe)
			return
		}
		controller := http.NewResponseController(w)
		if controller.SetWriteDeadline(time.Now().Add(30*time.Second)) != nil {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
		if _, err = w.Write(raw); err != nil || controller.Flush() != nil {
			return
		}
		// A fully read upstream receipt does not prove that HUB flushed it to
		// the owner. Keep the lease until the dedicated connection is closed.
		<-ctx.Done()
	})
	mux.HandleFunc("GET /v1/uploads/{id}/receipt", func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		id, actor, hash := r.PathValue("id"), r.Header.Get(uploadActorHeader), r.Header.Get(UploadSHA256Header)
		if !ValidID(id) || !actorPattern.MatchString(actor) || !digestPattern.MatchString(hash) {
			sendError(w, ErrInvalid)
			return
		}
		entry, err := inbox.Get(r.Context(), id)
		if err != nil {
			sendError(w, importError(err))
			return
		}
		if entry.ActorID != actor || entry.SHA256 != hash || entry.State != "received" {
			sendError(w, ErrConflict)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), UploadMaxDuration)
		defer cancel()
		release, err := control.AcquireTransfer(ctx)
		if err != nil {
			sendError(w, err)
			return
		}
		defer release()
		raw, _ := json.Marshal(backupimport.Response{Upload: entry, Replayed: true})
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
		if _, err := w.Write(raw); err != nil || http.NewResponseController(w).Flush() != nil {
			return
		}
		<-ctx.Done()
	})
	mux.HandleFunc("GET /v1/uploads/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		release, err := control.AcquireTransfer(r.Context())
		if err != nil {
			sendError(w, err)
			return
		}
		defer release()
		file, entry, err := inbox.OpenDownload(r.Context(), r.PathValue("id"))
		if err != nil {
			sendError(w, importError(err))
			return
		}
		defer file.Close()
		raw, _ := json.Marshal(entry)
		w.Header().Set(uploadHeader, base64.RawURLEncoding.EncodeToString(raw))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="dark-pro-upload-`+entry.ID+`.backup"`)
		controller := http.NewResponseController(w)
		if controller.SetWriteDeadline(time.Time{}) != nil {
			return
		}
		transfer := &downloadResponseWriter{ResponseWriter: w}
		http.ServeContent(transfer, r, "dark-pro-upload-"+entry.ID+".backup", time.Unix(entry.CreatedUnix, 0), file)
		if transfer.err != nil || transfer.written != entry.SizeBytes || controller.Flush() != nil {
			return
		}
		<-r.Context().Done()
	})
}

func (c *Client) UploadStatus(ctx context.Context) (backupimport.Status, error) {
	var out backupimport.Status
	err := c.call(ctx, "GET", "/uploads", nil, &out)
	return out, err
}
func (c *Client) CreateUpload(ctx context.Context, input backupimport.CreateRequest) (backupimport.Response, error) {
	var out backupimport.Response
	err := c.call(ctx, "POST", "/uploads", input, &out)
	return out, err
}
func (c *Client) GetUpload(ctx context.Context, id string) (backupimport.Upload, error) {
	var out backupimport.Upload
	if !ValidID(id) {
		return out, ErrInvalid
	}
	err := c.call(ctx, "GET", "/uploads/"+id, nil, &out)
	return out, err
}
func (c *Client) CancelUpload(ctx context.Context, id, actor string) (backupimport.Response, error) {
	return c.mutateUpload(ctx, id, actor, "cancel")
}
func (c *Client) RemoveUpload(ctx context.Context, id, actor string) (backupimport.Response, error) {
	return c.mutateUpload(ctx, id, actor, "remove")
}
func (c *Client) mutateUpload(ctx context.Context, id, actor, action string) (backupimport.Response, error) {
	var out backupimport.Response
	if !ValidID(id) || !actorPattern.MatchString(actor) {
		return out, ErrInvalid
	}
	err := c.call(ctx, "POST", "/uploads/"+id+"/"+action, map[string]string{"actor_id": actor}, &out)
	return out, err
}

type uploadLease struct {
	connection *downloadConnection
	transport  *http.Transport
}

func (l *uploadLease) Close() error {
	err := l.connection.Close()
	l.transport.CloseIdleConnections()
	return err
}

func (c *Client) Upload(ctx context.Context, id, actor string, size int64, hash string, body io.Reader) (*UploadResponse, error) {
	if !ValidID(id) || !actorPattern.MatchString(actor) || !digestPattern.MatchString(hash) || size < 1 || size > backupimport.MaxUploadBytes || body == nil {
		return nil, ErrInvalid
	}
	entry, err := c.GetUpload(ctx, id)
	if err != nil {
		return nil, err
	}
	if entry.ActorID != actor || entry.SizeBytes != size || entry.SHA256 != hash {
		return nil, ErrConflict
	}
	if entry.State == "received" {
		return c.completedUploadReceipt(ctx, id, actor, size, hash)
	}
	conn, err := c.dialDownload(ctx)
	if err != nil {
		return nil, ErrUnavailable
	}
	connection := &downloadConnection{Conn: conn, done: make(chan struct{})}
	var dial sync.Once
	transport := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			var selected net.Conn
			dial.Do(func() { selected = connection })
			if selected == nil {
				return nil, ErrUnavailable
			}
			return selected, nil
		},
		DisableCompression: true, ExpectContinueTimeout: time.Second,
		ResponseHeaderTimeout: time.Hour, MaxResponseHeaderBytes: 16 << 10,
		WriteBufferSize: 32 << 10, ReadBufferSize: 16 << 10,
	}
	lease := &uploadLease{connection: connection, transport: transport}
	retained := false
	defer func() {
		if !retained {
			_ = lease.Close()
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-connection.done:
		}
	}()
	// The inbound request owns body.Close. Letting Transport close it as soon
	// as the upload ends could interrupt HUB's connection before its receipt.
	req, err := http.NewRequestWithContext(ctx, "PUT", "http://backup-control"+ProtocolBase+"/uploads/"+id+"/content", struct{ io.Reader }{body})
	if err != nil {
		return nil, ErrInvalid
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Expect", "100-continue")
	req.Header.Set(uploadActorHeader, actor)
	req.Header.Set(UploadSHA256Header, hash)
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	if response.StatusCode != http.StatusOK {
		return nil, decodeError(response)
	}
	defer response.Body.Close()
	if response.ContentLength < 1 || response.ContentLength > 4096 {
		return nil, ErrUnsafe
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	var value backupimport.Response
	if err != nil || int64(len(raw)) != response.ContentLength || strictDecode(raw, &value) != nil ||
		value.Upload.ID != id || value.Upload.ActorID != actor || value.Upload.SizeBytes != size || value.Upload.SHA256 != hash {
		return nil, ErrUnsafe
	}
	retained = true
	return &UploadResponse{Response: value, Lease: lease}, nil
}

func (c *Client) completedUploadReceipt(ctx context.Context, id, actor string, size int64, hash string) (*UploadResponse, error) {
	conn, err := c.dialDownload(ctx)
	if err != nil {
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
	req, err := http.NewRequestWithContext(ctx, "GET", "http://backup-control"+ProtocolBase+"/uploads/"+id+"/receipt", nil)
	if err != nil {
		return nil, ErrInvalid
	}
	req.Header.Set(uploadActorHeader, actor)
	req.Header.Set(UploadSHA256Header, hash)
	if err = req.Write(conn); err != nil {
		return nil, ErrUnavailable
	}
	response, err := readDownloadResponse(bufio.NewReaderSize(conn, 16<<10), req)
	if err != nil {
		return nil, ErrUnavailable
	}
	if response.StatusCode != 200 {
		return nil, decodeError(response)
	}
	defer response.Body.Close()
	if response.ContentLength < 1 || response.ContentLength > 4096 {
		return nil, ErrUnsafe
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	var value backupimport.Response
	if err != nil || int64(len(raw)) != response.ContentLength || strictDecode(raw, &value) != nil || !value.Replayed ||
		value.Upload.ID != id || value.Upload.ActorID != actor || value.Upload.SizeBytes != size || value.Upload.SHA256 != hash ||
		value.Upload.State != "received" || value.Upload.Verification != "ciphertext_checksum_verified" || !value.Upload.Downloadable {
		return nil, ErrUnsafe
	}
	retained = true
	return &UploadResponse{Response: value, Lease: connection}, nil
}

func (c *Client) DownloadUpload(ctx context.Context, id string) (*UploadDownload, error) {
	if !ValidID(id) {
		return nil, ErrInvalid
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://backup-control"+ProtocolBase+"/uploads/"+id+"/download", nil)
	if err != nil {
		return nil, ErrInvalid
	}
	conn, err := c.dialDownload(ctx)
	if err != nil {
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
	if err = req.Write(conn); err != nil {
		return nil, ErrUnavailable
	}
	resp, err := readDownloadResponse(bufio.NewReaderSize(conn, 16<<10), req)
	if err != nil {
		return nil, ErrUnavailable
	}
	resp.Body = &downloadBody{ReadCloser: resp.Body, connection: connection}
	if resp.StatusCode != 200 {
		return nil, decodeError(resp)
	}
	raw, err := base64.RawURLEncoding.DecodeString(resp.Header.Get(uploadHeader))
	var entry backupimport.Upload
	if err != nil || len(raw) > 4096 || strictDecode(raw, &entry) != nil || entry.ID != id ||
		entry.State != "received" || entry.Verification != "ciphertext_checksum_verified" || !entry.Downloadable ||
		!digestPattern.MatchString(entry.SHA256) || entry.SizeBytes < 1 || entry.SizeBytes > backupimport.MaxUploadBytes || resp.ContentLength != entry.SizeBytes {
		resp.Body.Close()
		return nil, ErrUnsafe
	}
	retained = true
	return &UploadDownload{Upload: entry, Body: resp.Body, ContentLength: resp.ContentLength}, nil
}
