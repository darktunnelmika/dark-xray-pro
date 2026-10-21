package main

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// The list contains public identity/digest values only. Startup retains the
// existing explicit LAB/usage/TLS gates; no API accepts this allowlist.
func meteredRouteNodes(list, node, installation, binary string) ([]business.MeteredRouteNode, error) {
	if list == "" {
		return []business.MeteredRouteNode{{NodeID: node, InstallationID: installation, BinarySHA: binary}}, nil
	}
	if node != "" || installation != "" || binary != "" || len(list) > 16384 {
		return nil, errors.New("metered route allowlist conflicts with legacy scope or is too large")
	}
	d := json.NewDecoder(strings.NewReader(list))
	d.DisallowUnknownFields()
	var nodes []business.MeteredRouteNode
	if err := d.Decode(&nodes); err != nil {
		return nil, errors.New("invalid metered route allowlist")
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) || len(nodes) == 0 || len(nodes) > 64 {
		return nil, errors.New("invalid metered route allowlist length or trailing data")
	}
	return nodes, nil
}
