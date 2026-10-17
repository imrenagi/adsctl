package schema

import "testing"

func TestMutationSchemaRejectsUnsafePayloads(t *testing.T) {
	for _, input := range []string{`{"access_token":"secret"}`, `{"status":"DELETED"}`, `{"name":"x","adset_id":"123","creative":{"object_story_spec":{}}}`, `{"name":"x","daily_budget":2.5}`, `{"name":"x","daily_budget":100,"lifetime_budget":200}`} {
		p, err := Decode([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if Validate("ads", "create", p) == nil {
			t.Fatal("accepted", input)
		}
	}
	for _, input := range []string{`{} {}`, `{} garbage`, `[]`, `null`} {
		if _, err := Decode([]byte(input)); err == nil {
			t.Fatal("accepted", input)
		}
	}
}
func TestExactIntegerPayload(t *testing.T) {
	p, err := Decode([]byte(`{"name":"x","daily_budget":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = Validate("campaigns", "update", p); err != nil {
		t.Fatal(err)
	}
	if p["daily_budget"].(interface{ String() string }).String() != "9007199254740993" {
		t.Fatal(p)
	}
}

func TestObservedNativeSubsetComparison(t *testing.T) {
	want := map[string]any{"daily_budget": "2500", "targeting": map[string]any{"countries": []any{"ID"}}, "creative": map[string]any{"creative_id": "123"}}
	got := map[string]any{"daily_budget": 2500, "targeting": map[string]any{"countries": []any{"ID"}, "provider_default": true}, "creative": map[string]any{"id": "123", "name": "existing"}}
	if !MatchesPayload(want, got) {
		t.Fatal("native normalization rejected")
	}
	got["daily_budget"] = 2000
	if MatchesPayload(want, got) {
		t.Fatal("budget difference accepted")
	}
}

func TestCreativeUpdateIsMetadataOnly(t *testing.T) {
	if err := Validate("creatives", "update", map[string]any{"name": "Launch Video"}); err != nil {
		t.Fatal("metadata name update rejected:", err)
	}
	for _, p := range []map[string]any{
		{"object_story_spec": map[string]any{"page_id": "1"}},
		{"object_story_id": "1_2"},
		{"name": "x", "body": "changed copy"},
	} {
		if err := Validate("creatives", "update", p); err == nil {
			t.Fatal("creative content mutation accepted:", p)
		}
	}
}

func TestUnorderedTargetingFieldsCompareAsSets(t *testing.T) {
	planned := map[string]any{"targeting": map[string]any{
		"instagram_positions": []any{"reels", "story"},
		"publisher_platforms": []any{"facebook", "instagram"},
	}}
	reordered := map[string]any{"targeting": map[string]any{
		"instagram_positions": []any{"story", "reels"},
		"publisher_platforms": []any{"instagram", "facebook"},
	}}
	if path := FirstMismatch("adsets", planned, reordered); path != "" {
		t.Fatal("reordered set fields reported drift at", path)
	}
	// A genuinely changed placement must still be drift, reported at its field.
	changed := map[string]any{"targeting": map[string]any{
		"instagram_positions": []any{"story", "feed"},
		"publisher_platforms": []any{"instagram", "facebook"},
	}}
	if path := FirstMismatch("adsets", planned, changed); path != "targeting.instagram_positions" {
		t.Fatal("changed placement not reported at the field, got", path)
	}
}

func TestOrderedArraysStayPositional(t *testing.T) {
	planned := map[string]any{"adset_schedule": []any{map[string]any{"start": "1"}, map[string]any{"start": "2"}}}
	reordered := map[string]any{"adset_schedule": []any{map[string]any{"start": "2"}, map[string]any{"start": "1"}}}
	if path := FirstMismatch("adsets", planned, reordered); path != "adset_schedule[0].start" {
		t.Fatal("ordered array reordering was not rejected, got", path)
	}
}

func TestMismatchReportsNestedFieldPath(t *testing.T) {
	planned := map[string]any{"targeting": map[string]any{"geo_locations": map[string]any{"countries": []any{"ID"}}}}
	observed := map[string]any{"targeting": map[string]any{"geo_locations": map[string]any{"countries": []any{"US"}}}}
	if path := FirstMismatch("adsets", planned, observed); path != "targeting.geo_locations.countries[0]" {
		t.Fatal(path)
	}
}

func TestCreativeNameProviderSuffixIsNormalized(t *testing.T) {
	planned := map[string]any{"name": "Launch Video", "object_story_spec": map[string]any{"page_id": "1"}}
	observed := map[string]any{"name": "Launch Video - 2026-10-16", "object_story_spec": map[string]any{"page_id": "1"}}
	if path := FirstMismatch("creatives", planned, observed); path != "" {
		t.Fatal("provider name suffix reported drift at", path)
	}
	// A wholly different creative name is still drift.
	observed["name"] = "Something Else"
	if path := FirstMismatch("creatives", planned, observed); path != "name" {
		t.Fatal("different creative name accepted, got", path)
	}
	// Account/asset identity fields stay strict even when the name matches.
	observed["name"] = "Launch Video - 2026-10-16"
	observed["object_story_spec"] = map[string]any{"page_id": "2"}
	if path := FirstMismatch("creatives", planned, observed); path != "object_story_spec.page_id" {
		t.Fatal("asset identity drift accepted, got", path)
	}
	// Other resources keep strict name comparison.
	other := map[string]any{"name": "Launch Video - 2026-10-16"}
	if path := FirstMismatch("campaigns", map[string]any{"name": "Launch Video"}, other); path != "name" {
		t.Fatal("campaign name normalized, got", path)
	}
}
