package hubcore

import (
	"encoding/json"
	"net/http"
	"reflect"
	"regexp"
	"strings"
)

// The external API described as OpenAPI 3 (GET /api/v2/openapi.json, for a
// panel session or any partner or reseller key; ?api=partner|reseller picks
// one). Schemas come from the Go types the handlers answer with, so the
// description follows the code.

// Request and answer shapes that handlers build inline, named for the spec.
type (
	docOK struct {
		OK bool `json:"ok"`
	}
	docCredit struct {
		CreditGB float64 `json:"credit_gb"`
		Units    int64   `json:"units"`
		Note     string  `json:"note"`
	}
	docPanelCreate struct {
		ID          string       `json:"id"`       // empty: made up
		Name        string       `json:"name"`     //
		Username    string       `json:"username"` // empty: the id
		Password    string       `json:"password"` // empty: a random one, returned
		CreditGB    float64      `json:"credit_gb"`
		Units       int64        `json:"units"`
		Package     string       `json:"package"` // instead of credit_gb/units
		Policy      *APIPolicyIn `json:"policy"`
		TelegramIDs []int64      `json:"telegram_ids"`
		ExternalRef string       `json:"external_ref"`
		Note        string       `json:"note"`
		LowCreditGB *int64       `json:"low_credit_gb"`
		TermDays    int64        `json:"term_days"` // the panel runs this long (0: the package's, or no term)
		OnExpiry    string       `json:"on_expiry"` // block (customers keep working) | suspend (switched off after grace_days)
		GraceDays   int64        `json:"grace_days"`
	}
	docPanelRenew struct {
		Days      int64  `json:"days"`    // added from the end of the term, or from now when it has ended
		Package   string `json:"package"` // its days and its credit, like buying it again
		OnExpiry  string `json:"on_expiry"`
		GraceDays *int64 `json:"grace_days"`
	}
	docPanelPatch struct {
		Name        *string      `json:"name"`
		Note        *string      `json:"note"`
		ExternalRef *string      `json:"external_ref"`
		Enabled     *bool        `json:"enabled"`
		Policy      *APIPolicyIn `json:"policy"`
		TelegramIDs *[]int64     `json:"telegram_ids"`
		LowCreditGB *int64       `json:"low_credit_gb"`
		OnExpiry    string       `json:"on_expiry"`
		GraceDays   *int64       `json:"grace_days"`
	}
	docPassword struct {
		Password string `json:"password"` // empty: a random one
	}
	docPanelDeleted struct {
		OK             bool    `json:"ok"`
		ID             string  `json:"id"`
		ClientsDeleted int     `json:"clients_deleted"`
		ReleasedGB     float64 `json:"released_gb"`
		ReleasedUnits  int64   `json:"released_units"`
	}
	docSessions struct {
		OK    bool  `json:"ok"`
		Ended int64 `json:"ended"`
	}
	docLoginLink struct {
		URL      string `json:"url"`
		Expires  int64  `json:"expires_unix"`
		Username string `json:"username"`
	}
	docPackage struct {
		ID     string    `json:"id"`
		Name   string    `json:"name"`
		GB     int64     `json:"credit_gb"`
		Units  int64     `json:"units"`
		Price  int64     `json:"price"`
		Policy APIPolicy `json:"policy"`
	}
	docLedger struct {
		Items      []APILedgerRow `json:"items"`
		NextBefore int64          `json:"next_before"` // pass as before= for older rows
	}
	docEvents struct {
		Items     []APIEvent `json:"items"`
		NextAfter int64      `json:"next_after"` // pass as after= for newer events
	}
	docDeliveries struct {
		Items []APIDelivery `json:"items"`
	}
	docClientPage struct {
		Items  []APIClient `json:"items"`
		Total  int         `json:"total"`
		Limit  int         `json:"limit"`
		Offset int         `json:"offset"`
	}
	docCharge struct {
		GB    float64 `json:"gb"`
		Units int64   `json:"units"`
	}
	docBalance struct {
		CreditGB float64 `json:"credit_gb"`
		Units    int64   `json:"units"`
	}
	docClientChange struct {
		Client  APIClient  `json:"client"`
		Charge  docCharge  `json:"charge"`
		Refund  *docCharge `json:"refund,omitempty"`
		Balance docBalance `json:"balance"`
	}
	docClientDeleted struct {
		OK      bool       `json:"ok"`
		Name    string     `json:"name"`
		Refund  docCharge  `json:"refund"`
		Balance docBalance `json:"balance"`
	}
	docClientPatch struct {
		Enabled     *bool     `json:"enabled"`
		MaxIPs      *int      `json:"max_ips"`
		Inbounds    *[]string `json:"inbounds"`
		Note        *string   `json:"note"`
		DeviceLimit *int      `json:"device_limit"`
		TelegramID  *int64    `json:"telegram_id"`
	}
	docRenew struct {
		Plan    string  `json:"plan"` // empty: the customer's own
		GB      float64 `json:"gb"`
		Days    int64   `json:"days"`
		Product string  `json:"product"` // instead of plan/gb/days
	}
	docBulk struct {
		resClientIn
		Prefix string `json:"prefix"`
		Count  int    `json:"count"`
		Start  int    `json:"start"`
	}
	docBulkOut struct {
		Created []APIClient       `json:"created"`
		Charge  docCharge         `json:"charge"`
		Balance docBalance        `json:"balance"`
		Stopped map[string]string `json:"stopped,omitempty"` // when credit or a rule stopped it part way
	}
	docLinks struct {
		Name     string       `json:"name"`
		SubURL   string       `json:"sub_url"`
		LinksURL string       `json:"links_url"`
		Links    []ClientLink `json:"links"`
	}
	docUsageDay struct {
		Day  string `json:"day"`
		Up   int64  `json:"up_bytes"`
		Down int64  `json:"down_bytes"`
	}
	docUsage struct {
		Name string        `json:"name"`
		Days []docUsageDay `json:"days"`
	}
	docDevices struct {
		Name  string   `json:"name"`
		Limit int      `json:"limit"`
		Items []Device `json:"items"`
	}
	docIPs struct {
		Name   string     `json:"name"`
		MaxIPs int        `json:"max_ips"`
		Items  []ClientIP `json:"items"`
	}
	docOnline struct {
		Name  string   `json:"name"`
		Node  string   `json:"node"`
		IPs   []string `json:"ips"`
		Seen  int64    `json:"seen"`
		Today int64    `json:"today_bytes"`
	}
	docKeyInfo struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Scopes  []string `json:"scopes"`
		Expires int64    `json:"expires_unix"`
	}
	docMe struct {
		ID        string     `json:"id"`
		Name      string     `json:"name"`
		CreditGB  float64    `json:"credit_gb"`
		Units     int64      `json:"units"`
		Clients   int        `json:"clients"`
		Policy    APIPolicy  `json:"policy"`
		SubSeller bool       `json:"sub_seller"`
		SubBase   string     `json:"sub_base"`
		Locked    bool       `json:"locked"` // an overdue bill or an ended term stops sales and renewals
		Term      RepTerm    `json:"term"`
		Key       docKeyInfo `json:"key"`
	}
	docInbound struct {
		Tag      string   `json:"tag"`
		Protocol string   `json:"protocol"`
		Network  string   `json:"network"`
		Security string   `json:"security"`
		Enabled  bool     `json:"enabled"`
		Nodes    []string `json:"nodes"`
	}
	docPlan struct {
		ID              string   `json:"id"`
		Name            string   `json:"name"`
		Plan            string   `json:"plan"`
		GB              int64    `json:"gb"`
		Days            int64    `json:"days"`
		MaxIPs          int      `json:"max_ips"`
		Inbounds        []string `json:"inbounds"`
		Price           int64    `json:"price"`
		Category        string   `json:"category"`
		StartOnFirstUse bool     `json:"start_on_first_use"`
		NewOnly         bool     `json:"new_only"`
	}
	docSellerCreate struct {
		ID          string       `json:"id"`
		Name        string       `json:"name"`
		Username    string       `json:"username"`
		Password    string       `json:"password"`
		CreditGB    float64      `json:"credit_gb"`
		Units       int64        `json:"units"`
		Policy      *APIPolicyIn `json:"policy"`
		TelegramIDs []int64      `json:"telegram_ids"`
	}
	docSellerPatch struct {
		Name     *string      `json:"name"`
		Enabled  *bool        `json:"enabled"`
		Policy   *APIPolicyIn `json:"policy"`
		Password string       `json:"password"`
	}
	docSellerCredit struct {
		Seller  APISeller  `json:"seller"`
		Balance docBalance `json:"balance"`
	}
	docError struct {
		Detail    string `json:"detail"`
		Code      string `json:"code"`
		RequestID string `json:"request_id"`
	}
)

var docNames = map[string]string{"resClientIn": "ClientCreate", "QuoteIn": "QuoteRequest", "docBulk": "BulkCreate"}

// apiOp is one documented route.
type apiOp struct {
	API, Method, Path, Scope, Summary string
	In, Out                           any
	Array                             bool   // the answer is a list of Out
	Status                            int    // 0: 200
	Query                             string // "name: what, name: what"
	Binary                            string // a non-JSON answer's media type
}

func apiOps() []apiOp {
	pp, rp := "/api/v2/partner", "/api/v2/reseller"
	ops := []apiOp{
		{API: "partner", Method: "GET", Path: pp, Summary: "The key: limits, what is used and left, scopes", Out: PartnerKey{}},
		{API: "partner", Method: "GET", Path: pp + "/packages", Summary: "Packages this key may sell", Out: docPackage{}, Array: true},
		{API: "partner", Method: "GET", Path: pp + "/stats", Scope: scopePanelsRead, Summary: "Numbers over all its panels", Query: "days: period in days (default 30)", Out: PartnerStats{}},
		{API: "partner", Method: "GET", Path: pp + "/ledger", Scope: scopePanelsRead, Summary: "Credit added or taken back, every panel, newest first", Query: "before: an id from next_before, limit: 1-500", Out: docLedger{}},
		{API: "partner", Method: "POST", Path: pp + "/reps", Scope: scopePanelsWrite, Summary: "Make a reseller panel (credit needs credit:add); the answer has the password and a ready-to-send message", In: docPanelCreate{}, Out: PartnerPanel{}},
		{API: "partner", Method: "GET", Path: pp + "/reps", Scope: scopePanelsRead, Summary: "Its panels, newest first; X-Total-Count has the number that match", Query: "q: search, enabled: true|false, external_ref: exact, limit: up to 500 (none = all), offset", Out: PartnerPanel{}, Array: true},
		{API: "partner", Method: "GET", Path: pp + "/reps/{id}", Scope: scopePanelsRead, Summary: "One panel", Out: PartnerPanel{}},
		{API: "partner", Method: "PATCH", Path: pp + "/reps/{id}", Scope: scopePanelsWrite, Summary: "Change a panel; rules can only get tighter than the key's template or package", In: docPanelPatch{}, Out: PartnerPanel{}},
		{API: "partner", Method: "DELETE", Path: pp + "/reps/{id}", Scope: scopePanelsDelete, Summary: "Delete a panel; its unused credit can be handed out again", Query: "clients: 1 to delete its customers too (without it a panel with customers is refused, 409)", Out: docPanelDeleted{}},
		{API: "partner", Method: "POST", Path: pp + "/reps/{id}/credit", Scope: scopeCreditAdd, Summary: "Add credit", In: docCredit{}, Out: PartnerPanel{}},
		{API: "partner", Method: "POST", Path: pp + "/reps/{id}/renew", Scope: scopePanelsWrite, Summary: "Extend the panel's term by days, or by a package (its days and its credit); customers switched off at the end come back", In: docPanelRenew{}, Out: PartnerPanel{}},
		{API: "partner", Method: "POST", Path: pp + "/reps/{id}/credit/revoke", Scope: scopeCreditRevoke, Summary: "Take unused credit back (402 when the panel has less)", In: docCredit{}, Out: PartnerPanel{}},
		{API: "partner", Method: "GET", Path: pp + "/reps/{id}/ledger", Scope: scopePanelsRead, Summary: "A panel's credit history", Query: "before, limit", Out: docLedger{}},
		{API: "partner", Method: "GET", Path: pp + "/reps/{id}/overview", Scope: scopePanelsRead, Summary: "A panel's dashboard numbers", Out: APIOverview{}},
		{API: "partner", Method: "GET", Path: pp + "/reps/{id}/clients", Scope: scopeClientsRead, Summary: "A panel's customers (read only)", Query: "status: active|on_hold|expired|limited|disabled, q: search, limit, offset", Out: docClientPage{}},
		{API: "partner", Method: "POST", Path: pp + "/reps/{id}/enable", Scope: scopePanelsWrite, Summary: "Turn a panel on", Out: PartnerPanel{}},
		{API: "partner", Method: "POST", Path: pp + "/reps/{id}/disable", Scope: scopePanelsWrite, Summary: "Turn a panel off: no sign-in, no sales; its customers keep working", Out: PartnerPanel{}},
		{API: "partner", Method: "POST", Path: pp + "/reps/{id}/password", Scope: scopePanelsWrite, Summary: "A new password, random or chosen (10+ characters)", In: docPassword{}, Out: PartnerPanel{}},
		{API: "partner", Method: "POST", Path: pp + "/reps/{id}/sessions/revoke", Scope: scopePanelsWrite, Summary: "Sign the panel out everywhere", Out: docSessions{}},
		{API: "partner", Method: "POST", Path: pp + "/reps/{id}/login-link", Scope: scopePanelsWrite, Summary: "A one-time sign-in link, valid 10 minutes (not for a login with two-step sign-in)", Out: docLoginLink{}},
		{API: "partner", Method: "GET", Path: pp + "/reps/{id}/apikeys", Scope: scopePanelsRead, Summary: "The panel's reseller API keys", Out: ResKey{}, Array: true},
		{API: "partner", Method: "POST", Path: pp + "/reps/{id}/apikeys", Scope: scopePanelsWrite, Summary: "Make a reseller API key for the panel (the key is shown once)", In: ResKeyIn{}, Out: ResKey{}, Status: 201},
		{API: "partner", Method: "DELETE", Path: pp + "/reps/{id}/apikeys/{key}", Scope: scopePanelsWrite, Summary: "Delete one of the panel's reseller API keys", Out: docOK{}},

		{API: "reseller", Method: "GET", Path: rp + "/me", Scope: scopeAccountRead, Summary: "Credit, rules and the key", Out: docMe{}},
		{API: "reseller", Method: "GET", Path: rp + "/inbounds", Scope: scopeAccountRead, Summary: "Inbounds it may give", Out: docInbound{}, Array: true},
		{API: "reseller", Method: "GET", Path: rp + "/plans", Scope: scopeAccountRead, Summary: "Its published plans", Out: docPlan{}, Array: true},
		{API: "reseller", Method: "POST", Path: rp + "/quote", Scope: scopeAccountRead, Summary: "What a create or renew would cost and whether it would pass; changes nothing", In: QuoteIn{}, Out: Quote{}},
		{API: "reseller", Method: "GET", Path: rp + "/overview", Scope: scopeAccountRead, Summary: "Dashboard numbers", Out: APIOverview{}},
		{API: "reseller", Method: "GET", Path: rp + "/ledger", Scope: scopeAccountRead, Summary: "Credit history, newest first", Query: "before, limit", Out: docLedger{}},
		{API: "reseller", Method: "GET", Path: rp + "/clients", Scope: scopeResClientsR, Summary: "Customers, newest first", Query: "status: active|on_hold|expired|limited|disabled, q: search, limit: up to 500, offset", Out: docClientPage{}},
		{API: "reseller", Method: "POST", Path: rp + "/clients", Scope: scopeResClientsW, Summary: "Make a customer, paid from credit", In: resClientIn{}, Out: docClientChange{}, Status: 201},
		{API: "reseller", Method: "POST", Path: rp + "/clients/bulk", Scope: scopeResClientsW, Summary: "Make prefix-<n> customers; nothing is made unless every name is free and the credit covers all", In: docBulk{}, Out: docBulkOut{}, Status: 201},
		{API: "reseller", Method: "GET", Path: rp + "/clients/{name}", Scope: scopeResClientsR, Summary: "One customer", Out: APIClient{}},
		{API: "reseller", Method: "PATCH", Path: rp + "/clients/{name}", Scope: scopeResClientsW, Summary: "Change on/off, IP limit, inbounds, note, device limit, Telegram id", In: docClientPatch{}, Out: APIClient{}},
		{API: "reseller", Method: "DELETE", Path: rp + "/clients/{name}", Scope: scopeResClientsDel, Summary: "Delete; unused credit comes back when the rules say so", Out: docClientDeleted{}},
		{API: "reseller", Method: "POST", Path: rp + "/clients/{name}/renew", Scope: scopeResClientsW, Summary: "A new period; unused volume of the old one comes back", In: docRenew{}, Out: docClientChange{}},
		{API: "reseller", Method: "POST", Path: rp + "/clients/{name}/reset", Scope: scopeResClientsW, Summary: "Usage back to zero; a volume plan pays the used part", Out: docClientChange{}},
		{API: "reseller", Method: "POST", Path: rp + "/clients/{name}/revoke", Scope: scopeResClientsW, Summary: "New subscription link and credentials", Out: APIClient{}},
		{API: "reseller", Method: "POST", Path: rp + "/clients/{name}/enable", Scope: scopeResClientsW, Summary: "Turn on", Out: APIClient{}},
		{API: "reseller", Method: "POST", Path: rp + "/clients/{name}/disable", Scope: scopeResClientsW, Summary: "Turn off", Out: APIClient{}},
		{API: "reseller", Method: "GET", Path: rp + "/clients/{name}/links", Scope: scopeResClientsR, Summary: "Subscription address and every config link", Out: docLinks{}},
		{API: "reseller", Method: "GET", Path: rp + "/clients/{name}/qr.png", Scope: scopeResClientsR, Summary: "QR code of the subscription address", Query: "size: 128-1024", Binary: "image/png"},
		{API: "reseller", Method: "GET", Path: rp + "/clients/{name}/usage", Scope: scopeResClientsR, Summary: "Traffic per day, oldest first", Query: "days: 1-90 (default 30)", Out: docUsage{}},
		{API: "reseller", Method: "GET", Path: rp + "/clients/{name}/devices", Scope: scopeResClientsR, Summary: "Devices seen", Out: docDevices{}},
		{API: "reseller", Method: "DELETE", Path: rp + "/clients/{name}/devices/{hwid}", Scope: scopeResClientsW, Summary: "Forget a device", Out: docOK{}},
		{API: "reseller", Method: "GET", Path: rp + "/clients/{name}/ips", Scope: scopeResClientsR, Summary: "Addresses seen in the last 30 days", Out: docIPs{}},
		{API: "reseller", Method: "GET", Path: rp + "/online", Scope: scopeResClientsR, Summary: "Customers online now", Out: docOnline{}, Array: true},
		{API: "reseller", Method: "GET", Path: rp + "/sellers", Scope: scopeSellers, Summary: "Sub-sellers", Out: APISeller{}, Array: true},
		{API: "reseller", Method: "POST", Path: rp + "/sellers", Scope: scopeSellers, Summary: "Make a sub-seller with a first move of credit; rules never looser than its own", In: docSellerCreate{}, Out: APISeller{}, Status: 201},
		{API: "reseller", Method: "PATCH", Path: rp + "/sellers/{id}", Scope: scopeSellers, Summary: "Change a sub-seller", In: docSellerPatch{}, Out: APISeller{}},
		{API: "reseller", Method: "POST", Path: rp + "/sellers/{id}/credit", Scope: scopeSellers, Summary: "Move credit to a sub-seller", In: docCredit{}, Out: docSellerCredit{}},
		{API: "reseller", Method: "POST", Path: rp + "/sellers/{id}/credit/revoke", Scope: scopeSellers, Summary: "Take a sub-seller's unused credit back", In: docCredit{}, Out: docSellerCredit{}},
	}
	for _, api := range []struct{ name, prefix string }{{"partner", pp}, {"reseller", rp}} {
		ops = append(ops,
			apiOp{API: api.name, Method: "GET", Path: api.prefix + "/events", Scope: scopeEvents, Summary: "Its events, oldest first (poll with after=next_after)", Query: "after: an event id, types: comma list, limit: 1-500", Out: docEvents{}},
			apiOp{API: api.name, Method: "GET", Path: api.prefix + "/webhook", Scope: scopeEvents, Summary: "Its webhook", Out: APIWebhook{}},
			apiOp{API: api.name, Method: "PUT", Path: api.prefix + "/webhook", Scope: scopeEvents, Summary: "Set its webhook; a new one (or rotate_secret) returns the signing secret once", In: APIWebhookIn{}, Out: APIWebhook{}},
			apiOp{API: api.name, Method: "DELETE", Path: api.prefix + "/webhook", Scope: scopeEvents, Summary: "Remove its webhook (events stay readable)", Out: docOK{}},
			apiOp{API: api.name, Method: "POST", Path: api.prefix + "/webhook/test", Scope: scopeEvents, Summary: "Send a ping event now", Out: APIDelivery{}},
			apiOp{API: api.name, Method: "GET", Path: api.prefix + "/webhook/deliveries", Scope: scopeEvents, Summary: "Recent delivery tries", Query: "limit: 1-200", Out: docDeliveries{}},
		)
	}
	return ops
}

// openAPISpec builds the description for "partner", "reseller" or both ("").
func openAPISpec(base, which string) map[string]any {
	comps := map[string]any{}
	paths := map[string]map[string]any{}
	pathParam := regexp.MustCompile(`\{([a-z]+)\}`)
	errRef := map[string]any{"description": "error", "content": map[string]any{"application/json": map[string]any{"schema": schemaOf(reflect.TypeOf(docError{}), comps)}}}
	for _, op := range apiOps() {
		if which != "" && op.API != which {
			continue
		}
		o := map[string]any{"summary": op.Summary, "tags": []string{op.API}, "operationId": strings.ToLower(op.Method) + strings.NewReplacer("/api/v2/", "_", "/", "_", "{", "", "}", "", "-", "_", ".", "_").Replace(op.Path)}
		if op.Scope != "" {
			o["description"] = "Scope: `" + op.Scope + "`"
		}
		var params []any
		for _, m := range pathParam.FindAllStringSubmatch(op.Path, -1) {
			params = append(params, map[string]any{"name": m[1], "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
		}
		if op.Query != "" {
			for _, q := range strings.Split(op.Query, ", ") {
				name, desc, _ := strings.Cut(q, ": ")
				params = append(params, map[string]any{"name": name, "in": "query", "description": desc, "schema": map[string]any{"type": "string"}})
			}
		}
		if op.Method != "GET" {
			params = append(params, map[string]any{"name": "Idempotency-Key", "in": "header", "description": "the same key and request within 24 hours runs once and gets the first answer again", "schema": map[string]any{"type": "string"}})
		}
		if params != nil {
			o["parameters"] = params
		}
		if op.In != nil {
			o["requestBody"] = map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": schemaOf(reflect.TypeOf(op.In), comps)}}}
		}
		status := op.Status
		if status == 0 {
			status = 200
		}
		ok := map[string]any{"description": "OK"}
		switch {
		case op.Binary != "":
			ok["content"] = map[string]any{op.Binary: map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}}}
		case op.Out != nil:
			sch := schemaOf(reflect.TypeOf(op.Out), comps)
			if op.Array {
				sch = map[string]any{"type": "array", "items": sch}
			}
			ok["content"] = map[string]any{"application/json": map[string]any{"schema": sch}}
		}
		o["responses"] = map[string]any{itoa(status): ok, "400": errRef, "401": errRef, "402": errRef, "403": errRef, "404": errRef, "409": errRef, "422": errRef, "429": errRef}
		if paths[op.Path] == nil {
			paths[op.Path] = map[string]any{}
		}
		paths[op.Path][strings.ToLower(op.Method)] = o
	}
	title, desc := "Dark Xray Pro external API", "Partner API (dp_ keys): make and run reseller panels. Reseller API (dr_ keys): a reseller's own bot or site sells and runs its customers."
	return map[string]any{
		"openapi":  "3.0.3",
		"info":     map[string]any{"title": title, "version": HubVersion, "description": desc},
		"servers":  []any{map[string]any{"url": base}},
		"security": []any{map[string]any{"bearer": []string{}}},
		"tags":     []any{map[string]any{"name": "partner", "description": "Bearer dp_<id>_<secret>"}, map[string]any{"name": "reseller", "description": "Bearer dr_<id>_<secret>"}},
		"paths":    paths,
		"components": map[string]any{
			"schemas":         comps,
			"securitySchemes": map[string]any{"bearer": map[string]any{"type": "http", "scheme": "bearer"}},
		},
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

var rawMessageType = reflect.TypeOf(json.RawMessage{})

// schemaOf describes a Go type; named structs become components.
func schemaOf(t reflect.Type, comps map[string]any) map[string]any {
	if t == rawMessageType {
		return map[string]any{"type": "object"}
	}
	switch t.Kind() {
	case reflect.Pointer:
		s := map[string]any{}
		for k, v := range schemaOf(t.Elem(), comps) {
			s[k] = v
		}
		if _, ref := s["$ref"]; ref {
			return map[string]any{"allOf": []any{s}, "nullable": true}
		}
		s["nullable"] = true
		return s
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return map[string]any{"type": "integer"}
	case reflect.Int64, reflect.Uint64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaOf(t.Elem(), comps)}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaOf(t.Elem(), comps)}
	case reflect.Struct:
		name := t.Name()
		if n, ok := docNames[name]; ok {
			name = n
		}
		name = strings.TrimPrefix(name, "doc")
		if name == "" {
			return structSchema(t, comps)
		}
		if _, done := comps[name]; !done {
			comps[name] = map[string]any{} // placeholder against loops
			comps[name] = structSchema(t, comps)
		}
		return map[string]any{"$ref": "#/components/schemas/" + name}
	}
	return map[string]any{}
}

func structSchema(t reflect.Type, comps map[string]any) map[string]any {
	props := map[string]any{}
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		for i := range t.NumField() {
			f := t.Field(i)
			tag := f.Tag.Get("json")
			name, _, _ := strings.Cut(tag, ",")
			if f.Anonymous && name == "" {
				walk(f.Type)
				continue
			}
			if !f.IsExported() || name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			props[name] = schemaOf(f.Type, comps)
		}
	}
	walk(t)
	return map[string]any{"type": "object", "properties": props}
}

func openAPIRoute(s *Store, mux *http.ServeMux, adminToken string) {
	mux.HandleFunc("GET /api/v2/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		which := r.URL.Query().Get("api")
		if which != "partner" && which != "reseller" {
			which = ""
		}
		allowed := false
		if c, err := s.partnerCaller(r); err == nil {
			allowed = true
			if which == "" {
				which = c.Kind
			}
		} else if c, err := s.resellerCaller(r); err == nil {
			allowed = true
			if which == "" {
				which = c.Kind
			}
		} else if _, ok := authorize(s, adminToken, r); ok {
			allowed = true
		}
		if !allowed {
			requestID(w, r)
			apiErr(w, http.StatusUnauthorized, codeUnauthorized, "sign in to the panel, or send a partner or reseller key")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAPISpec(s.panelOrigin(r), which))
	})
}
