package business

import (
	"encoding/json"
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// TLSCredentials is write-only creation-plan input. It is never embedded in
// NativeInbound, a stored public plan, a receipt, or a preview response.
type TLSCredentials struct {
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
}

// Preserve explicit null separately from omission. Unknown/duplicate fields
// are rejected, including inside the credential object; no arbitrary paths or
// lower-level Xray JSON can enter this authoring surface.
func (in *NativeInboundPlanInput) UnmarshalJSON(raw []byte) error {
	type wire NativeInboundPlanInput
	var value wire
	if err := usagewire.Decode(raw, &value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	*in = NativeInboundPlanInput(value)
	for name, value := range fields {
		if strings.EqualFold(name, "tls_credentials") {
			in.tlsCredentialsNull = strings.TrimSpace(string(value)) == "null"
		}
	}
	return nil
}

func planProfileError(err error) error {
	var specDetail *inboundspec.FieldError
	if errors.As(err, &specDetail) {
		return planError("", "profile.spec."+specDetail.Path, specDetail.Code, ErrInvalid)
	}
	var detail *xrayconfig.ProfileValidationError
	if errors.As(err, &detail) {
		field := detail.Field
		if !strings.HasPrefix(field, "tls_credentials") {
			field = "profile." + field
		}
		return planError("", field, detail.Code, ErrInvalid)
	}
	return planError("", "profile", "profile_invalid", ErrInvalid)
}

func normalizePlanTLS(in *NativeInboundPlanInput) error {
	if in.Profile.Spec != nil {
		if in.TLSCredentials != nil || in.tlsCredentialsNull {
			return planError("", "tls_credentials", "spec_credentials_must_be_nested", ErrInvalid)
		}
		return normalizePlanSpecInput(in)
	}
	security, err := xrayconfig.NormalizeProfileSecurity(in.Profile.Security)
	if err != nil {
		return planProfileError(err)
	}
	in.Profile.Security = security
	if in.tlsCredentialsNull {
		return planError("", "tls_credentials", "tls_credentials_invalid", ErrInvalid)
	}
	if in.TLSCredentials == nil {
		if security == "tls" && in.ExpectedPlanRevision == 0 {
			return planError("", "tls_credentials", "tls_credentials_required", ErrInvalid)
		}
		return nil
	}
	if security != "tls" {
		return planError("", "tls_credentials", "tls_credentials_forbidden", ErrInvalid)
	}
	value, _, err := xrayconfig.NormalizeTLSMaterial(&xrayconfig.TLSMaterial{
		CertificatePEM: in.TLSCredentials.CertificatePEM, PrivateKeyPEM: in.TLSCredentials.PrivateKeyPEM,
	})
	if err != nil {
		return planProfileError(err)
	}
	// Canonicalization is independent of time, so exact historical events can
	// replay after renewal/expiration without reading or replacing current keys.
	in.TLSCredentials = &TLSCredentials{CertificatePEM: value.CertificatePEM, PrivateKeyPEM: value.PrivateKeyPEM}
	return nil
}

func planTLSMaterial(input *TLSCredentials) *xrayconfig.TLSMaterial {
	if input == nil {
		return nil
	}
	return &xrayconfig.TLSMaterial{CertificatePEM: input.CertificatePEM, PrivateKeyPEM: input.PrivateKeyPEM}
}

// Populate public fields from authenticated private storage. No certificate
// PEM or private-key representation is copied to the public DTO.
func publicInboundProfile(in *NativeInbound, profile xrayconfig.Profile) error {
	if profile.Spec != nil {
		return publicAuthoringProfile(in, profile)
	}
	in.Transport, in.Security = profile.Transport, profile.Security
	in.GRPCService, in.RealityTarget = profile.GRPCService, profile.RealityTarget
	in.ServerNames, in.ShortIDs, in.Options = profile.ServerNames, profile.ShortIDs, profile.Options
	in.TLS = nil
	if profile.Security == "tls" {
		_, metadata, err := xrayconfig.NormalizeTLSMaterial(profile.TLS)
		if err != nil || in.PublicKey != "" {
			return ErrUnavailable
		}
		in.TLS = metadata
	}
	return nil
}

func hasInboundTLSAuthoring(transport string, options *xrayconfig.ProfileOptions) bool {
	return transport == "ws" || transport == "httpupgrade" || options != nil && (options.WebSocket != nil || options.HTTPUpgrade != nil)
}

func planEnvelopeVersion(plan NativeInboundPlan) int {
	if plan.Profile.Spec != nil {
		return 4
	}
	if plan.Profile.Security == "tls" || plan.Profile.TLS != nil || hasInboundTLSAuthoring(plan.Profile.Transport, plan.Profile.Options) {
		return 3
	}
	if planNeedsExtendedEnvelope(plan) {
		return 2
	}
	return 1
}
