package backupcontrol

import (
	"context"
	"net/http"
	"sort"
)

const MaxRestores = 128

// A restore is an explicit replacement, not an upload or a checksum receipt.
// No path, command, private recovery key, service or destination comes from HTTP.
type RestoreRequest struct {
	EventID      string `json:"event_id"`
	ActorID      string `json:"actor_id"`
	SourceKind   string `json:"source_kind"`
	SourceID     string `json:"source_id"`
	SHA256       string `json:"sha256"`
	Confirmation string `json:"confirmation"`
}
type RestoreJob struct {
	ID           string `json:"id"`
	EventID      string `json:"event_id"`
	ActorID      string `json:"actor_id"`
	SourceKind   string `json:"source_kind"`
	SourceID     string `json:"source_id"`
	SHA256       string `json:"sha256"`
	State        string `json:"state"`
	Phase        string `json:"phase"`
	CreatedUnix  int64  `json:"created_unix"`
	FinishedUnix int64  `json:"finished_unix"`
	Revision     uint64 `json:"revision"`
	ErrorCode    string `json:"error_code"`
	TableCount   int    `json:"table_count"`
	DataRestored bool   `json:"data_restored"`
}
type RestoreResponse struct {
	Job      RestoreJob `json:"job"`
	Replayed bool       `json:"replayed"`
}
type RestoreStatus struct {
	Enabled bool         `json:"enabled"`
	Jobs    []RestoreJob `json:"jobs"`
}
type RestoreControl interface {
	Available() bool
	Create(context.Context, RestoreRequest) (RestoreResponse, error)
}

func validRestoreRequest(r RestoreRequest) bool {
	return validActorEvent(r.ActorID, r.EventID) && (r.SourceKind == "archive" || r.SourceKind == "upload") &&
		ValidID(r.SourceID) && digestPattern.MatchString(r.SHA256) && r.Confirmation == "RESTORE"
}
func validateRestoreJob(j RestoreJob) error {
	if !ValidID(j.ID) || !validRestoreRequest(RestoreRequest{EventID: j.EventID, ActorID: j.ActorID, SourceKind: j.SourceKind, SourceID: j.SourceID, SHA256: j.SHA256, Confirmation: "RESTORE"}) ||
		j.CreatedUnix <= 0 || j.FinishedUnix < 0 || j.Revision < 1 || j.Revision > 9007199254740991 || j.TableCount < 0 || j.TableCount > 1024 ||
		j.ErrorCode != "" && !reasonPattern.MatchString(j.ErrorCode) {
		return ErrInvalid
	}
	switch j.State {
	case "queued", "running", "succeeded", "failed", "recovery_required":
	default:
		return ErrInvalid
	}
	switch j.Phase {
	case "queued", "validating", "stopping_hub", "applying", "rolling_back", "starting_restored", "starting_original", "complete":
	default:
		return ErrInvalid
	}
	if Terminal(j.State) && (j.Phase != "complete" || j.FinishedUnix <= 0) {
		return ErrInvalid
	}
	if !Terminal(j.State) && (j.Phase == "complete" || j.FinishedUnix != 0) {
		return ErrInvalid
	}
	if j.State == "queued" && j.Phase != "queued" || j.State != "queued" && j.Phase == "queued" {
		return ErrInvalid
	}
	if j.State == "succeeded" && (!j.DataRestored || j.TableCount == 0 || j.ErrorCode != "") {
		return ErrInvalid
	}
	if j.DataRestored && j.Phase != "starting_restored" && j.State != "succeeded" {
		return ErrInvalid
	}
	return nil
}
func activeRestore(d journal) bool {
	for _, j := range d.Restores {
		if !Terminal(j.State) {
			return true
		}
	}
	return false
}
func (s *Store) CreateRestore(ctx context.Context, r RestoreRequest) (RestoreResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return RestoreResponse{}, e
	}
	if !validRestoreRequest(r) {
		return RestoreResponse{}, ErrInvalid
	}
	digest := requestDigest("restore", r)
	if old, ok := s.data.Receipts[r.EventID]; ok {
		if old.Kind != "restore" || old.Digest != digest {
			return RestoreResponse{}, ErrConflict
		}
		return RestoreResponse{Job: s.data.Restores[old.JobID], Replayed: true}, nil
	}
	if activeJobs(s.data) || s.transfers != 0 || len(s.pins) != 0 || len(s.data.Deleting) != 0 {
		return RestoreResponse{}, ErrBusy
	}
	if len(s.data.Restores) >= MaxRestores || len(s.data.Receipts) >= MaxReceipts {
		return RestoreResponse{}, ErrCapacity
	}
	id, e := newID()
	if e != nil {
		return RestoreResponse{}, e
	}
	d := cloneJournal(s.data)
	if _, ok := d.Restores[id]; ok {
		return RestoreResponse{}, ErrConflict
	}
	j := RestoreJob{ID: id, EventID: r.EventID, ActorID: r.ActorID, SourceKind: r.SourceKind, SourceID: r.SourceID, SHA256: r.SHA256, State: "queued", Phase: "queued", CreatedUnix: s.clock().Unix(), Revision: 1}
	d.Restores[id] = j
	d.Receipts[r.EventID] = receipt{Kind: "restore", Digest: digest, JobID: id}
	if e = s.commit(d); e != nil {
		return RestoreResponse{}, e
	}
	return RestoreResponse{Job: j}, nil
}

// FindRestoreReceipt supports response-loss reconciliation even if the original
// source was subsequently removed. A completed request never replaces data twice.
func (s *Store) FindRestoreReceipt(ctx context.Context, r RestoreRequest) (RestoreResponse, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return RestoreResponse{}, false, e
	}
	if !validRestoreRequest(r) {
		return RestoreResponse{}, false, ErrInvalid
	}
	old, ok := s.data.Receipts[r.EventID]
	if !ok {
		return RestoreResponse{}, false, nil
	}
	if old.Kind != "restore" || old.Digest != requestDigest("restore", r) {
		return RestoreResponse{}, true, ErrConflict
	}
	return RestoreResponse{Job: s.data.Restores[old.JobID], Replayed: true}, true, nil
}
func (s *Store) RestoreJobs(ctx context.Context) ([]RestoreJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return nil, e
	}
	out := make([]RestoreJob, 0, len(s.data.Restores))
	for _, j := range s.data.Restores {
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedUnix == out[j].CreatedUnix {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedUnix > out[j].CreatedUnix
	})
	return out, nil
}
func (s *Store) UpdateRestore(ctx context.Context, id string, change func(*RestoreJob) error) (RestoreJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return RestoreJob{}, e
	}
	old, ok := s.data.Restores[id]
	if !ok {
		return RestoreJob{}, ErrNotFound
	}
	if change == nil {
		return RestoreJob{}, ErrInvalid
	}
	j := old
	if e := change(&j); e != nil {
		return RestoreJob{}, e
	}
	if j.ID != old.ID || j.EventID != old.EventID || j.ActorID != old.ActorID || j.SourceKind != old.SourceKind || j.SourceID != old.SourceID || j.SHA256 != old.SHA256 || j.CreatedUnix != old.CreatedUnix {
		return RestoreJob{}, ErrInvalid
	}
	if j == old {
		return old, nil
	}
	if Terminal(old.State) {
		return RestoreJob{}, ErrConflict
	}
	// The first restored start is a one-way barrier. Never rewind a financial
	// database after it may have admitted new work, including after a crash.
	if old.Phase == "starting_restored" && j.Phase != "starting_restored" && !(j.State == "succeeded" && j.Phase == "complete") {
		return RestoreJob{}, ErrConflict
	}
	if old.Phase == "starting_original" && j.Phase != "starting_original" && !(j.State == "failed" && j.Phase == "complete") {
		return RestoreJob{}, ErrConflict
	}
	if old.State != "queued" && j.State == "queued" {
		return RestoreJob{}, ErrConflict
	}
	j.Revision = old.Revision + 1
	if validateRestoreJob(j) != nil {
		return RestoreJob{}, ErrInvalid
	}
	d := cloneJournal(s.data)
	d.Restores[id] = j
	if e := s.commit(d); e != nil {
		return RestoreJob{}, e
	}
	return j, nil
}
func (s *Store) ArchiveForRestore(ctx context.Context, id, hash string) (Archive, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return Archive{}, e
	}
	a, ok := s.data.Archives[id]
	if !ok {
		return Archive{}, ErrNotFound
	}
	if a.SHA256 != hash {
		return Archive{}, ErrConflict
	}
	if s.data.Deleting[id] || !a.Downloadable || s.data.Jobs[a.JobID].State != "succeeded" {
		return Archive{}, ErrBusy
	}
	return a, nil
}
func attachRestoreHandlers(mux *http.ServeMux, store *Store, control RestoreControl) {
	mux.HandleFunc("GET /v1/restores", func(w http.ResponseWriter, r *http.Request) {
		jobs, e := store.RestoreJobs(r.Context())
		if e != nil {
			sendError(w, e)
			return
		}
		send(w, 200, RestoreStatus{Enabled: control != nil && control.Available(), Jobs: jobs})
	})
	mux.HandleFunc("POST /v1/restores", func(w http.ResponseWriter, r *http.Request) {
		var q RestoreRequest
		if readRequest(w, r, &q) != nil || !validRestoreRequest(q) {
			sendError(w, ErrInvalid)
			return
		}
		if control == nil {
			sendError(w, ErrUnavailable)
			return
		}
		out, e := control.Create(r.Context(), q)
		if e != nil {
			sendError(w, e)
			return
		}
		send(w, 202, out)
	})
}
func (c *Client) RestoreStatus(ctx context.Context) (RestoreStatus, error) {
	var out RestoreStatus
	e := c.call(ctx, "GET", "/restores", nil, &out)
	return out, e
}
func (c *Client) CreateRestore(ctx context.Context, q RestoreRequest) (RestoreResponse, error) {
	var out RestoreResponse
	if !validRestoreRequest(q) {
		return out, ErrInvalid
	}
	e := c.call(ctx, "POST", "/restores", q, &out)
	return out, e
}
