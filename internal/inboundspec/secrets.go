package inboundspec

import (
	"sort"
	"strconv"
	"strings"
)

func isSecretField(path, key string) bool {
	switch strings.ToLower(key) {
	case "password", "pass", "privatekey", "private_key", "secretkey", "secret", "secrets", "decryption", "testseed", "key", "keyfile", "certificate", "certificatefile", "echserverkeys", "mldsa65seed", "presharedkey", "headerprotectionkey", "masterkeylog":
		return true
	}
	// Storage templates can embed credentials in URLs, bodies, or headers.
	// Redact the whole template instead of guessing which strings are tokens.
	if path == "stream.xdriveSettings" && key == "template" {
		return true
	}
	// Header maps may contain bearer tokens or cookies. Their values must not
	// reach receipts, revision snapshots, or a browser after the initial write.
	if key == "headers" || key == "header" && strings.Contains(path, "template.auth") {
		return true
	}
	if key == "value" && strings.HasPrefix(path, "stream.finalmask.udp.") && strings.HasSuffix(path, ".settings") {
		return true
	}
	if key == "url" && (strings.HasPrefix(path, "stream.finalmask.") || path == "stream.hysteriaSettings.masquerade") {
		return true
	}
	return false
}

func specRoots(s *Spec) map[string]map[string]any {
	return map[string]map[string]any{"settings": s.Settings, "stream": s.Stream, "tls": s.TLS, "reality": s.Reality, "client": s.Client}
}

func walkSecrets(v any, path string, remove bool, paths *[]string) {
	switch m := v.(type) {
	case map[string]any:
		for k, x := range m {
			p := pathJoin(path, k)
			if isSecretField(path, k) {
				if x != nil {
					*paths = append(*paths, p)
				}
				if remove {
					delete(m, k)
				}
			} else {
				walkSecrets(x, p, remove, paths)
			}
		}
	case []any:
		for i, x := range m {
			walkSecrets(x, pathJoin(path, strconv.Itoa(i)), remove, paths)
		}
	}
}

// Public removes credentials rather than masking them with a recoverable value.
// SecretPaths describes presence only. Malformed non-JSON inputs fail closed.
func Public(input Spec) PublicSpec {
	s, e := cloneSpec(input)
	if e != nil {
		return PublicSpec{Spec: Spec{Version: Version}, SecretPaths: []string{}}
	}
	if e = ensureStableRowIDs(&s); e != nil {
		return PublicSpec{Spec: Spec{Version: Version}, SecretPaths: []string{}}
	}
	paths := []string{}
	for name, m := range specRoots(&s) {
		walkSecrets(m, name, true, &paths)
	}
	sort.Strings(paths)
	return PublicSpec{Spec: s, SecretPaths: paths}
}

func SecretPaths(input Spec) []string {
	s, e := cloneSpec(input)
	if e != nil {
		return []string{}
	}
	paths := []string{}
	for name, m := range specRoots(&s) {
		walkSecrets(m, name, false, &paths)
	}
	sort.Strings(paths)
	return paths
}

// MergeSecrets preserves an omitted write-only field. An explicit empty value,
// null, empty list, or removed array entry never resurrects that credential.
// Protocol, security, and transport changes stop incompatible secret reuse.
func MergeSecrets(previous, incoming Spec) (Spec, error) {
	old, e := cloneSpec(previous)
	if e != nil {
		return Spec{}, e
	}
	out, e := cloneSpec(incoming)
	if e != nil {
		return Spec{}, e
	}
	if e = ensureStableRowIDs(&old); e != nil {
		return Spec{}, e
	}
	if out.Settings == nil {
		out.Settings = map[string]any{}
	}
	if out.Stream == nil {
		out.Stream = map[string]any{}
	}
	if out.TLS == nil {
		out.TLS = map[string]any{}
	}
	if out.Reality == nil {
		out.Reality = map[string]any{}
	}
	if out.Client == nil {
		out.Client = map[string]any{}
	}
	oldRoots, newRoots := specRoots(&old), specRoots(&out)
	for _, path := range SecretPaths(old) {
		parts := strings.Split(path, ".")
		scope := parts[0]
		if len(parts) < 2 {
			continue
		}
		switch scope {
		case "settings", "client":
			if old.Protocol != out.Protocol {
				continue
			}
		case "stream":
			if old.Protocol != out.Protocol || old.Network != out.Network {
				continue
			}
		case "tls", "reality":
			if old.Security != out.Security {
				continue
			}
		}
		if e = mergeSecretPath(oldRoots[scope], newRoots[scope], parts[1:]); e != nil {
			return Spec{}, e
		}
	}
	return out, nil
}

func mergeSecretPath(old, current any, path []string) error {
	if len(path) == 0 {
		return nil
	}
	switch o := old.(type) {
	case map[string]any:
		n, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		if t := str(o, "type"); t != "" && str(n, "type") != t {
			return nil
		}
		v, exists := o[path[0]]
		if !exists {
			return nil
		}
		c, present := n[path[0]]
		if len(path) == 1 {
			if !present {
				n[path[0]] = v
			}
			return nil
		}
		if !present {
			switch child := v.(type) {
			case map[string]any:
				c = map[string]any{}
			case []any:
				a := make([]any, len(child))
				for i, x := range child {
					switch row := x.(type) {
					case map[string]any:
						a[i] = map[string]any{}
						if id := str(row, "id"); id != "" {
							a[i].(map[string]any)["id"] = id
						}
						if kind := str(row, "type"); kind != "" {
							a[i].(map[string]any)["type"] = kind
						}
					case []any:
						a[i] = []any{}
					}
				}
				c = a
			default:
				return nil
			}
			n[path[0]] = c
		}
		return mergeSecretPath(v, c, path[1:])
	case []any:
		n, ok := current.([]any)
		if !ok {
			return nil
		}
		i, e := strconv.Atoi(path[0])
		if e != nil || i < 0 || i >= len(o) {
			return nil
		}
		if len(path) == 1 {
			return nil
		}
		index := i
		if row, ok := o[i].(map[string]any); ok && str(row, "id") != "" {
			index = -1
			for j, v := range n {
				if m, ok := v.(map[string]any); ok && str(m, "id") == str(row, "id") {
					index = j
					break
				}
			}
			if index < 0 {
				if i < len(n) {
					if m, ok := n[i].(map[string]any); ok && str(m, "id") == "" && (str(row, "type") == "" || str(m, "type") == str(row, "type")) && !hasPath(m, path[1:]) {
						return invalid("spec", "secret_reindex_requires_material")
					}
				}
				return nil
			}
		}
		if index >= len(n) {
			return nil
		}
		return mergeSecretPath(o[i], n[index], path[1:])
	}
	return nil
}

func hasPath(value any, path []string) bool {
	if len(path) == 0 {
		return true
	}
	switch v := value.(type) {
	case map[string]any:
		x, ok := v[path[0]]
		return ok && hasPath(x, path[1:])
	case []any:
		i, e := strconv.Atoi(path[0])
		return e == nil && i >= 0 && i < len(v) && hasPath(v[i], path[1:])
	}
	return false
}
