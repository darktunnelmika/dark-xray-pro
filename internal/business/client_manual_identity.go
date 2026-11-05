package business

import "regexp"

// Canonical RFC 4122 UUID input; optional to preserve historical event fingerprints.
var manualClientUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
