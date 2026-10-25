package business

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// Normalize public mirrors without inventing credentials. Validation of an edit
// happens after its omitted write-only fields have been restored from storage.
func normalizePlanSpecInput(in *NativeInboundPlanInput) error {
	if in.Profile.Options != nil {
		return planError("", "profile.options", "spec_legacy_options_conflict", ErrInvalid)
	}
	raw, err := json.Marshal(in.Profile.Spec)
	if err != nil || len(raw) > inboundspec.MaxSpecBytes {
		return planError("", "profile.spec", "spec_too_large", ErrInvalid)
	}
	var spec inboundspec.Spec
	if usagewire.Decode(raw, &spec) != nil {
		return planError("", "profile.spec", "spec_invalid", ErrInvalid)
	}
	if spec.Version == 0 {
		spec.Version = inboundspec.Version
	}
	spec.Protocol = strings.ToLower(strings.TrimSpace(spec.Protocol))
	spec.Network = strings.ToLower(strings.TrimSpace(spec.Network))
	spec.Security = strings.ToLower(strings.TrimSpace(spec.Security))
	if spec.Network == "raw" {
		spec.Network = "tcp"
	}
	for _, info := range inboundspec.Catalog() {
		if info.Protocol == spec.Protocol {
			if spec.Network == "" {
				spec.Network = info.Networks[0]
			}
			if spec.Security == "" {
				spec.Security = info.Securities[0]
			}
		}
	}
	in.Profile.Spec = &spec
	mirrorAuthoringInput(&in.Profile, spec)
	return nil
}

func specString(m map[string]any, key string) string { value, _ := m[key].(string); return value }
func inboundInputEqual(a, b InboundInput) bool {
	if a.Spec == nil && b.Spec == nil {
		return reflect.DeepEqual(a, b)
	}
	x, xe := json.Marshal(a)
	y, ye := json.Marshal(b)
	return xe == nil && ye == nil && bytes.Equal(x, y)
}
func specStrings(m map[string]any, key string) []string {
	var result []string
	switch values := m[key].(type) {
	case []string:
		result = append(result, values...)
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok {
				result = append(result, text)
			}
		}
	}
	return result
}
func mirrorAuthoringInput(in *InboundInput, spec inboundspec.Spec) {
	in.Transport, in.Security, in.Options = spec.Network, spec.Security, nil
	in.GRPCService, in.RealityTarget = "", ""
	in.ServerNames, in.ShortIDs = nil, nil
	if grpc, ok := spec.Stream["grpcSettings"].(map[string]any); ok {
		in.GRPCService = specString(grpc, "serviceName")
	}
	if spec.Security == "reality" {
		in.RealityTarget = specString(spec.Reality, "target")
		in.ServerNames, in.ShortIDs = specStrings(spec.Reality, "serverNames"), specStrings(spec.Reality, "shortIds")
	} else if spec.Security == "tls" {
		if name := specString(spec.TLS, "serverName"); name != "" {
			in.ServerNames = []string{name}
		}
		if name := specString(spec.Client, "serverName"); name != "" && len(in.ServerNames) == 0 {
			in.ServerNames = []string{name}
		}
	}
}

func publicAuthoringProfile(in *NativeInbound, private xrayconfig.Profile) error {
	if private.Spec == nil {
		return ErrUnavailable
	}
	public := inboundspec.Public(*private.Spec)
	in.Spec, in.SpecSecretPaths, in.TLS = &public.Spec, public.SecretPaths, nil
	mirrorAuthoringInput(&in.InboundInput, public.Spec)
	in.PublicKey = specString(public.Client, "publicKey")
	return nil
}

// These fixed keys only validate a new draft. They are never persisted, handed
// to a runtime, or exposed as a deployable configuration. Saving generates fresh
// material from crypto/rand, independently of this preview.
func previewAuthoringSpec(input inboundspec.Spec) (inboundspec.Spec, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return inboundspec.Spec{}, ErrInvalid
	}
	var spec inboundspec.Spec
	if usagewire.Decode(raw, &spec) != nil {
		return inboundspec.Spec{}, ErrInvalid
	}
	if spec.Settings == nil {
		spec.Settings = map[string]any{}
	}
	if spec.Reality == nil {
		spec.Reality = map[string]any{}
	}
	fill := func(m map[string]any, field string, size int, url bool) {
		if specString(m, field) != "" {
			return
		}
		key := bytes.Repeat([]byte{1}, size)
		if url {
			m[field] = base64.RawURLEncoding.EncodeToString(key)
		} else {
			m[field] = base64.StdEncoding.EncodeToString(key)
		}
	}
	if spec.Security == "reality" {
		fill(spec.Reality, "privateKey", 32, true)
		if len(specStrings(spec.Reality, "shortIds")) == 0 {
			spec.Reality["shortIds"] = []any{"0101010101010101"}
		}
	}
	switch spec.Protocol {
	case "wireguard":
		fill(spec.Settings, "secretKey", 32, false)
	case "shadowsocks":
		n := 32
		if specString(spec.Settings, "method") == "2022-blake3-aes-128-gcm" {
			n = 16
		}
		fill(spec.Settings, "password", n, false)
	case "mtproto":
		fill(spec.Settings, "secret", 32, true)
	case "amneziawg":
		server, ok := spec.Settings["server"].(map[string]any)
		if !ok {
			server = map[string]any{}
			spec.Settings["server"] = server
		}
		fill(server, "privateKey", 32, false)
	}
	return inboundspec.Normalize(spec)
}

func materializeAuthoringProfile(private *xrayconfig.Profile, public *NativeInbound, input inboundspec.Spec) error {
	spec, err := inboundspec.EnsureSecrets(input)
	if err == nil {
		spec, err = inboundspec.Normalize(spec)
	}
	if err != nil {
		return planProfileError(err)
	}
	private.Spec, private.Transport, private.Security = &spec, spec.Network, spec.Security
	if err = xrayconfig.ValidateProfile(*private); err != nil {
		return planProfileError(err)
	}
	return publicAuthoringProfile(public, *private)
}

func validateAuthoringPlanTx(ctx context.Context, tx *sql.Tx, key []byte, in NativeInboundPlanInput) (NativeInboundPlan, xrayconfig.Profile, xrayconfig.CreationTopology, error) {
	var private xrayconfig.Profile
	var topology xrayconfig.CreationTopology
	now := time.Now().Unix()
	plan := NativeInboundPlan{Nodes: in.Nodes, PlanRevision: in.ExpectedPlanRevision + 1, LastEventID: in.EventID,
		Reason: in.Reason, Status: nativePlanStatus(in.Nodes), CreatedUnix: now, UpdatedUnix: now, Editable: true,
		Profile: NativeInbound{InboundInput: in.Profile, Revision: 1}}
	if _, err := planAEAD(key); err != nil {
		return plan, private, topology, err
	}
	var spec inboundspec.Spec
	var err error
	if in.ExpectedPlanRevision == 0 {
		var exists, total int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_inbounds WHERE id=?`, in.Profile.ID).Scan(&exists); err != nil {
			return plan, private, topology, err
		}
		if exists != 0 {
			return plan, private, topology, planError("", "profile.id", "inbound_id_exists", ErrConflict)
		}
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_inbounds`).Scan(&total); err != nil {
			return plan, private, topology, err
		}
		if total >= 256 {
			return plan, private, topology, planError("", "profile.id", "inbound_limit", ErrConflict)
		}
		spec, err = previewAuthoringSpec(*in.Profile.Spec)
	} else {
		old, e := readNativeInboundPlan(ctx, tx, key, in.Profile.ID)
		if e != nil {
			return plan, private, topology, e
		}
		if old.PlanRevision != in.ExpectedPlanRevision || old.InboundRevision != in.ExpectedInboundRevision {
			return plan, private, topology, planError("", "expected_plan_revision", "plan_revision_conflict", ErrConflict)
		}
		existing, original, e := editablePlanProfile(ctx, tx, key, old)
		if e != nil {
			return plan, private, topology, e
		}
		if original.Spec == nil {
			return plan, private, topology, planError("", "profile.spec", "legacy_profile_requires_legacy_editor", ErrConflict)
		}
		if e = checkEndpointPresence(in, old); e != nil {
			return plan, private, topology, e
		}
		previous := *original.Spec
		// Credentials from an unrelated protocol/security mode must never leak
		// into a newly selected protocol. Within a mode, omission preserves them.
		if previous.Protocol != in.Profile.Spec.Protocol {
			previous.Settings, previous.Client = nil, nil
		}
		if previous.Security != in.Profile.Spec.Security {
			previous.TLS, previous.Reality = nil, nil
		}
		spec, err = inboundspec.MergeSecrets(previous, *in.Profile.Spec)
		if err == nil {
			spec, err = inboundspec.Normalize(spec)
		}
		plan.CreatedUnix, plan.Profile.Revision = old.CreatedUnix, existing.Revision
		if err == nil && (!reflect.DeepEqual(original.Spec, &spec) || existing.Name != in.Profile.Name) {
			plan.Profile.Revision++
		}
	}
	if err != nil {
		return plan, private, topology, planProfileError(err)
	}
	private = xrayconfig.Profile{InboundID: in.Profile.ID, Transport: spec.Network, Security: spec.Security, Spec: &spec, Users: []xrayconfig.User{}}
	if err = xrayconfig.ValidateProfile(private); err != nil {
		return plan, private, topology, planProfileError(err)
	}
	if err = publicAuthoringProfile(&plan.Profile, private); err != nil {
		return plan, private, topology, err
	}
	plan.InboundRevision = plan.Profile.Revision
	if err = verifyPlanCollisions(ctx, tx, key, in); err != nil {
		return plan, private, topology, err
	}
	topology, err = compileNativeInboundPlan(private, in.Nodes)
	return plan, private, topology, err
}
