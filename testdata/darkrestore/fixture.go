// Package restorefixture embeds the synthetic snapshot schema and records used
// by tests, including binaries run outside the source checkout by systemd CI.
package restorefixture

import _ "embed"

//go:embed single-customer.sql
var SQL string
