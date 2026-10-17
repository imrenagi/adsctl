package schema

import (
	"fmt"
	"strings"
)

// MatchesPayload reports whether every planned field matches the observed
// native read. Provider-normalized set fields are compared without regard to
// element order.
func MatchesPayload(expected, observed map[string]any) bool {
	return FirstMismatch("", expected, observed) == ""
}

// FirstMismatch returns the dotted path of the first planned field that does
// not match the observed native read, or "" when every planned field matches.
// A path (rather than a bare bool) lets callers report which field drifted.
//
// resource scopes provider normalization: Meta may append a suffix to a
// creative's name after create, so creative names compare by prefix. Set-valued
// fields (for example placement lists under targeting) compare by membership;
// every other array stays positional.
func FirstMismatch(resource string, expected, observed map[string]any) string {
	return diffPayload(resource, "", expected, observed)
}

func diffPayload(resource, path string, expected, observed map[string]any) string {
	for k, v := range expected {
		field := joinPath(path, k)
		actual, ok := observed[k]
		if k == "creative" {
			if want, wok := v.(map[string]any); wok {
				if got, gok := actual.(map[string]any); gok {
					id := got["id"]
					if id == nil {
						id = got["creative_id"]
					}
					if fmt.Sprint(want["creative_id"]) != fmt.Sprint(id) {
						return field
					}
					continue
				}
			}
		}
		if !ok {
			return field
		}
		if k == "name" && resource == "creatives" {
			if want, wok := v.(string); wok {
				if got, gok := actual.(string); gok && nameNormalized(want, got) {
					continue
				}
			}
		}
		if mismatch := matchesValue(resource, field, v, actual); mismatch != "" {
			return mismatch
		}
	}
	return ""
}

func matchesValue(resource, path string, want, got any) string {
	switch v := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return path
		}
		return diffPayload(resource, path, v, g)
	case []any:
		g, ok := got.([]any)
		if !ok || len(v) != len(g) {
			return path
		}
		if isSetValued(path) {
			if matchesSet(resource, path, v, g) {
				return ""
			}
			return path
		}
		for i := range v {
			if mismatch := matchesValue(resource, fmt.Sprintf("%s[%d]", path, i), v[i], g[i]); mismatch != "" {
				return mismatch
			}
		}
		return ""
	default:
		if fmt.Sprint(want) == fmt.Sprint(got) {
			return ""
		}
		return path
	}
}

// matchesSet compares two arrays as unordered sets. Duplicates in the planned
// value must each be matched by a distinct observed element.
func matchesSet(resource, path string, want, got []any) bool {
	used := make([]bool, len(got))
	for _, w := range want {
		found := false
		for i, g := range got {
			if used[i] {
				continue
			}
			if matchesValue(resource, path, w, g) == "" {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// nameNormalized reports whether an observed provider name is the planned name
// with an appended provider marker. Meta appends a date/hash suffix to some
// creative names, so a leading match followed by a separator is treated as the
// same name; a wholly different name still fails.
func nameNormalized(planned, observed string) bool {
	if planned == observed {
		return true
	}
	rest, ok := strings.CutPrefix(observed, planned)
	if !ok || rest == "" {
		return false
	}
	switch rest[0] {
	case ' ', '-', '_', '(', '|':
		return true
	default:
		return false
	}
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// setValuedFields lists native fields Meta treats as unordered sets. The
// provider may return these in a normalized order that differs from the planned
// order, so comparison checks membership instead of position. The registry is
// intentionally narrow: only add fields the provider documents as sets, and
// keep strictly ordered arrays positional.
var setValuedFields = map[string]bool{
	"special_ad_categories":                true,
	"targeting.publisher_platforms":        true,
	"targeting.facebook_positions":         true,
	"targeting.instagram_positions":        true,
	"targeting.messenger_positions":        true,
	"targeting.audience_network_positions": true,
	"targeting.device_platforms":           true,
}

func isSetValued(path string) bool {
	return setValuedFields[path]
}
