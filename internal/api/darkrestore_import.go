package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

const restoreBase = "/api/v1/workspace/dark-restore"

type restoreStage struct {
	path   string
	review darkrestore.Review
	digest string
}
type restoreHandoffStage struct {
	path   string
	digest string
	public []byte
}

// Called once, before the browser listener starts. Only this local configuration
// selects a snapshot. HTTP callers may reference it by its canonical review hash.
func (s *Server) StageDarkRestoreImport(ctx context.Context, path, sha string, asOf int64) error {
	if s.darkRestoreStage != nil || len(s.inboundKey) != 32 || !s.clientOwnershipV1Enabled || s.business == nil || asOf <= 0 || asOf > time.Now().Unix() {
		return errors.New("invalid restore staging configuration")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return darkrestore.ErrUnsafeSource
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil || !dir.IsDir() || dir.Mode().Perm()&0077 != 0 {
		return darkrestore.ErrUnsafeSource
	}
	review, err := darkrestore.BuildReview(ctx, path, sha, time.Unix(asOf, 0))
	if err != nil {
		return err
	}
	digest, err := darkrestore.CanonicalReviewSHA256(review)
	if err != nil {
		return err
	}
	if _, err = darkrestore.ReadEventHistory(ctx, path, review); err != nil {
		return err
	}
	if err = s.business.EnableDarkRestoreImportV1(ctx); err != nil {
		return err
	}
	s.darkRestoreReceipts = true
	s.darkRestoreStage = &restoreStage{path, review, digest}
	return nil
}

type restoreQuoteRequest struct {
	ReviewSHA string                               `json:"review_sha256"`
	Mappings  []business.DarkRestoreGroupMappingV1 `json:"mappings"`
}
type restoreCommitRequest struct {
	restoreQuoteRequest
	EventID   string `json:"event_id"`
	QuoteHash string `json:"quote_hash"`
	QuoteUnix int64  `json:"quote_unix"`
}

func (s *Server) attachDarkRestoreImport(mux *http.ServeMux) {
	stage := s.darkRestoreStage
	if !s.darkRestoreReceipts {
		return
	}
	handle := func(pattern string, write bool, run func(http.ResponseWriter, *http.Request, string)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeBrowser(w, r, write, true)
			if !ok {
				return
			}
			if r.URL.RawQuery != "" || r.URL.ForceQuery {
				businessError(w, business.ErrInvalid)
				return
			}
			w.Header().Set("Cache-Control", "private, no-store")
			run(w, r, session.Token)
		})
	}
	handle("GET "+restoreBase+"/recent", false, func(w http.ResponseWriter, r *http.Request, token string) {
		recent, err := s.business.RecentDarkRestoreImports(r.Context(), token)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, map[string]any{"imports": recent})
	})
	handle("GET "+restoreBase+"/receipts/{event}/history/{offset}", false, func(w http.ResponseWriter, r *http.Request, token string) {
		offset, err := strconv.Atoi(r.PathValue("offset"))
		if err != nil || strconv.Itoa(offset) != r.PathValue("offset") {
			businessError(w, business.ErrInvalid)
			return
		}
		page, err := s.business.DarkRestoreEventHistory(r.Context(), token, r.PathValue("event"), s.inboundKey, offset, 20)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, page)
	})
	handle("GET "+restoreBase+"/receipts/{event}", false, func(w http.ResponseWriter, r *http.Request, token string) {
		result, err := s.business.DarkRestoreImportReceiptV1(r.Context(), token, r.PathValue("event"))
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, result)
	})
	if handoff := s.darkRestoreHandoff; handoff != nil {
		handle("GET "+restoreBase+"/handoff", false, func(w http.ResponseWriter, r *http.Request, token string) {
			verified, err := darkrestore.VerifySignedLocalHandoff(handoff.path, handoff.digest, handoff.public)
			if err != nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			proof, err := verified.Settlement()
			if err != nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			respond(w, 200, map[string]any{"handoff_sha256": handoff.digest, "source_settlement": proof})
		})
		handle("GET "+restoreBase+"/handoff/readiness", false, func(w http.ResponseWriter, r *http.Request, token string) {
			verified, err := darkrestore.VerifySignedLocalHandoff(handoff.path, handoff.digest, handoff.public)
			if err != nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			proof, err := verified.Settlement()
			if err != nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			readiness, err := s.business.DarkRestoreCutoverReadinessV1(r.Context(), proof.SourceSHA, proof.SourceID)
			if err != nil {
				businessError(w, err)
				return
			}
			if readiness.SourceScope == "single_local_source_epoch" && readiness.SourceEpochSettled && readiness.FiniteQuotaRemainingBytes != strconv.FormatInt(proof.Remaining, 10) {
				businessError(w, business.ErrUnavailable)
				return
			}
			respond(w, 200, readiness)
		})
		stagedClient := func(ctx context.Context) (string, error) {
			verified, err := darkrestore.VerifySignedLocalHandoff(handoff.path, handoff.digest, handoff.public)
			if err != nil {
				return "", business.ErrUnavailable
			}
			proof, err := verified.Settlement()
			if err != nil {
				return "", business.ErrUnavailable
			}
			state, err := s.business.DarkRestoreCutoverReadinessV1(ctx, proof.SourceSHA, proof.SourceID)
			return state.ClientID, err
		}
		handle("GET "+restoreBase+"/handoff/promotion", false, func(w http.ResponseWriter, r *http.Request, token string) {
			client, err := stagedClient(r.Context())
			if err != nil {
				businessError(w, err)
				return
			}
			receipt, err := s.business.DarkRestorePreparedPromotionReceipt(r.Context(), token, client)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, receipt)
		})
		for _, route := range []struct {
			suffix  string
			preview bool
		}{{"/promotion/preview", true}, {"/promotion", false}} {
			preview := route.preview
			handle("POST "+restoreBase+"/handoff"+route.suffix, true, func(w http.ResponseWriter, r *http.Request, token string) {
				client, err := stagedClient(r.Context())
				if err != nil {
					businessError(w, err)
					return
				}
				s.prepareDarkRestorePromotion(w, r, token, client, handoff.digest, preview)
			})
		}
		handle("GET "+restoreBase+"/handoff/quarantine/settlement", false, func(w http.ResponseWriter, r *http.Request, token string) {
			client, err := stagedClient(r.Context())
			if err != nil {
				businessError(w, err)
				return
			}
			e, err := s.business.ProjectDarkRestoreNativeClosedCycleEvidenceV1(r.Context(), token, client)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, 200, map[string]any{"scope": e.Scope, "client_id": e.ClientID, "plan_sha256": e.PlanSHA,
				"final_report_sha256": e.FinalReportSHA, "sealed_cycle_recorded": e.SealedCycleRecorded,
				"customer_uplink_bytes": strconv.FormatInt(e.CycleUplink, 10), "customer_downlink_bytes": strconv.FormatInt(e.CycleDownlink, 10),
				"authorizes_mutation": e.AuthorizesMutation})
		})
		handle("GET "+restoreBase+"/handoff/quarantine/admission/evidence", false, func(w http.ResponseWriter, r *http.Request, token string) {
			client, err := stagedClient(r.Context())
			if err != nil {
				businessError(w, err)
				return
			}
			evidence, err := s.business.ProjectDarkRestoreNativeAdmissionEvidenceV1(r.Context(), token, client)
			if err != nil {
				businessError(w, err)
				return
			}
			response := struct {
				business.DarkRestoreNativeAdmissionEvidenceV1
				SystemProbe *business.DarkRestoreSystemProbeStatusV1 `json:"system_probe,omitempty"`
			}{DarkRestoreNativeAdmissionEvidenceV1: evidence}
			if s.darkRestoreProbeEndpoint != nil {
				status, statusErr := s.business.ProjectDarkRestoreSystemProbeV1(r.Context(), token, client, evidence.PlanSHA)
				if statusErr != nil {
					businessError(w, statusErr)
					return
				}
				response.SystemProbe = &status
			}
			respond(w, 200, response)
		})
		for _, method := range []string{"GET", "POST"} {
			handle(method+" "+restoreBase+"/handoff/quarantine/connection-check", method == "POST", func(w http.ResponseWriter, r *http.Request, token string) {
				if s.customerConnectionEndpoint == nil {
					respond(w, 200, map[string]any{"enabled": false})
					return
				}
				verified, err := darkrestore.VerifySignedLocalHandoff(handoff.path, handoff.digest, handoff.public)
				if err != nil {
					businessError(w, business.ErrUnavailable)
					return
				}
				proof, err := verified.Settlement()
				if err != nil {
					businessError(w, business.ErrUnavailable)
					return
				}
				state, err := s.business.DarkRestoreCutoverReadinessV1(r.Context(), proof.SourceSHA, proof.SourceID)
				if err != nil {
					businessError(w, err)
					return
				}
				var result business.DarkRestoreConnectionStatus
				if r.Method != http.MethodPost {
					result, err = s.business.DarkRestoreConnectionStatus(r.Context(), token, state.ClientID)
				} else {
					var input business.DarkRestoreConnectionInput
					if decode(w, r, 2048, &input) != nil {
						businessError(w, business.ErrInvalid)
						return
					}
					result, err = s.business.QueueDarkRestoreConnection(r.Context(), s.inboundKey, token, state.ClientID, input, *s.customerConnectionEndpoint)
				}
				if err != nil {
					businessError(w, err)
					return
				}
				respond(w, 200, struct {
					Enabled bool `json:"enabled"`
					business.DarkRestoreConnectionStatus
				}{true, result})
			})
		}
		handle("POST "+restoreBase+"/handoff/quarantine/admission", true, func(w http.ResponseWriter, r *http.Request, token string) {
			var input struct {
				PlanSHA string `json:"plan_sha256"`
				EventID string `json:"event_id"`
			}
			if decode(w, r, 2048, &input) != nil {
				businessError(w, business.ErrInvalid)
				return
			}
			verified, err := darkrestore.VerifySignedLocalHandoff(handoff.path, handoff.digest, handoff.public)
			if err != nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			proof, err := verified.Settlement()
			if err != nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			state, err := s.business.DarkRestoreCutoverReadinessV1(r.Context(), proof.SourceSHA, proof.SourceID)
			if err != nil {
				businessError(w, err)
				return
			}
			result, err := s.business.AuthorizeDarkRestoreAdmission(r.Context(), s.inboundKey, token, state.ClientID, input.PlanSHA, input.EventID)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, 200, map[string]any{"event_id": result.EventID, "epoch_id": result.Registered.Epoch.ID, "expires_unix_nano": result.Registered.Birth.Lease.Expires, "scope": "metered_zero_receipt_admission", "runtime_applied": false})
		})
		handle("POST "+restoreBase+"/handoff/quarantine", true, func(w http.ResponseWriter, r *http.Request, token string) {
			verified, err := darkrestore.VerifySignedLocalHandoff(handoff.path, handoff.digest, handoff.public)
			if err != nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			proof, err := verified.Settlement()
			if err != nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			state, err := s.business.DarkRestoreCutoverReadinessV1(r.Context(), proof.SourceSHA, proof.SourceID)
			if err != nil {
				businessError(w, err)
				return
			}
			s.approveQuarantine(w, r, token, state.ClientID)
		})
		handle("POST "+restoreBase+"/handoff/prepare", true, func(w http.ResponseWriter, r *http.Request, token string) {
			var req struct {
				HandoffSHA      string `json:"handoff_sha256"`
				EventID         string `json:"event_id"`
				InboundID       uint64 `json:"inbound_id"`
				PlanRevision    uint64 `json:"plan_revision"`
				InboundRevision uint64 `json:"inbound_revision"`
			}
			var raw json.RawMessage
			if decode(w, r, 4096, &raw) != nil || usagewire.Decode(raw, &req) != nil || req.HandoffSHA != handoff.digest {
				businessError(w, business.ErrInvalid)
				return
			}
			verified, err := darkrestore.VerifySignedLocalHandoff(handoff.path, handoff.digest, handoff.public)
			if err != nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			proof, err := verified.Settlement()
			if err != nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			state, err := s.business.DarkRestoreCutoverReadinessV1(r.Context(), proof.SourceSHA, proof.SourceID)
			if err != nil {
				businessError(w, err)
				return
			}
			result, err := s.business.DeliverPreparedDarkRestoreSubscriptionV1(r.Context(), s.inboundKey, token, state.ClientID, business.DarkRestorePreparedDeliveryInputV1{
				EventID: req.EventID, InboundID: req.InboundID, ExpectedPlanRevision: req.PlanRevision, ExpectedInboundRevision: req.InboundRevision, Format: "uri",
			})
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, 200, map[string]any{"scope": result.Scope, "client_id": state.ClientID, "content": string(result.Subscription.Content), "applied": result.Applied, "path_verified": result.Subscription.PathVerified})
		})
		handle("POST "+restoreBase+"/handoff/attach", true, func(w http.ResponseWriter, r *http.Request, token string) {
			var req struct {
				HandoffSHA string `json:"handoff_sha256"`
			}
			var raw json.RawMessage
			if decode(w, r, 4096, &raw) != nil || usagewire.Decode(raw, &req) != nil || req.HandoffSHA != handoff.digest {
				businessError(w, business.ErrInvalid)
				return
			}
			verified, err := darkrestore.VerifySignedLocalHandoff(handoff.path, handoff.digest, handoff.public)
			if err != nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			receipt, err := s.business.AttachSignedLocalSettlementV1(r.Context(), token, verified)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, 200, receipt)
		})
	}
	if stage == nil {
		return
	}
	handle("GET "+restoreBase+"/stage", false, func(w http.ResponseWriter, r *http.Request, token string) {
		respond(w, 200, map[string]any{"review_sha256": stage.digest, "review": stage.review})
	})
	read := func(w http.ResponseWriter, r *http.Request, target any) bool {
		var raw json.RawMessage
		if decode(w, r, 4<<20, &raw) != nil || usagewire.Decode(raw, target) != nil {
			businessError(w, business.ErrInvalid)
			return false
		}
		return true
	}
	intent := func(req restoreQuoteRequest) (business.DarkRestoreImportIntentV1, error) {
		if req.ReviewSHA != stage.digest {
			return business.DarkRestoreImportIntentV1{}, business.ErrConflict
		}
		return business.DarkRestoreImportIntentV1{Review: stage.review, Mappings: req.Mappings}, nil
	}
	handle("POST "+restoreBase+"/quote", true, func(w http.ResponseWriter, r *http.Request, token string) {
		var req restoreQuoteRequest
		if !read(w, r, &req) {
			return
		}
		in, err := intent(req)
		if err != nil {
			businessError(w, err)
			return
		}
		result, err := s.business.QuoteDarkRestoreImportV1(r.Context(), token, in)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, result)
	})
	handle("POST "+restoreBase+"/commit", true, func(w http.ResponseWriter, r *http.Request, token string) {
		var req restoreCommitRequest
		if !read(w, r, &req) {
			return
		}
		in, err := intent(req.restoreQuoteRequest)
		if err != nil {
			businessError(w, err)
			return
		}
		// Full offline source verification can exceed the ordinary UI deadline.
		const budget = 3 * time.Minute
		if err = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(budget + 3*time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			businessError(w, business.ErrUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), budget)
		defer cancel()
		result, err := s.business.CommitDarkRestoreImportV1(ctx, token, stage.path, s.inboundKey, business.DarkRestoreImportCommitV1{DarkRestoreImportIntentV1: in, EventID: req.EventID, QuoteHash: req.QuoteHash, QuoteUnix: req.QuoteUnix})
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, result)
	})

}
