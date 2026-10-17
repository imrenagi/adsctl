// Package schema defines the deliberately supported native mutation subset.
package schema

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"adsctl/internal/config"
	"adsctl/internal/output"
)

type Field struct {
	Type     string   `json:"type"`
	Required bool     `json:"required_on_create,omitempty"`
	Enum     []string `json:"enum,omitempty"`
}

func Fields(resource, operation string) map[string]Field {
	if operation != "create" && operation != "update" {
		return nil
	}
	common := map[string]string{"name": "string", "adlabels": "array"}
	switch resource {
	case "campaigns":
		common["status"] = "string"
		for _, k := range strings.Fields("objective buying_type bid_strategy start_time stop_time") {
			common[k] = "string"
		}
		for _, k := range strings.Fields("daily_budget lifetime_budget spend_cap") {
			common[k] = "integer"
		}
		for _, k := range strings.Fields("special_ad_categories special_ad_category_country pacing_type") {
			common[k] = "array"
		}
		common["is_adset_budget_sharing_enabled"] = "boolean"
	case "adsets":
		for _, k := range strings.Fields("status billing_event optimization_goal bid_strategy start_time end_time destination_type dsa_beneficiary dsa_payor") {
			common[k] = "string"
		}
		for _, k := range strings.Fields("daily_budget lifetime_budget bid_amount daily_min_spend_target daily_spend_cap lifetime_min_spend_target lifetime_spend_cap") {
			common[k] = "integer"
		}
		for _, k := range strings.Fields("targeting promoted_object bid_constraints") {
			common[k] = "object"
		}
		for _, k := range strings.Fields("attribution_spec pacing_type frequency_control_specs adset_schedule") {
			common[k] = "array"
		}
		common["is_dynamic_creative"] = "boolean"
		if operation == "create" {
			common["campaign_id"] = "id"
		}
	case "ads":
		for _, k := range strings.Fields("status conversion_domain ad_schedule_start_time ad_schedule_end_time") {
			common[k] = "string"
		}
		common["creative"] = "object"
		common["tracking_specs"] = "object"
		if operation == "create" {
			common["adset_id"] = "id"
		}
	case "creatives":
		if operation == "create" {
			for _, k := range strings.Fields("object_story_id image_hash image_url title body link_url object_url url_tags instagram_user_id") {
				common[k] = "string"
			}
			for _, k := range strings.Fields("object_story_spec asset_feed_spec degrees_of_freedom_spec call_to_action contextual_multi_ads") {
				common[k] = "object"
			}
		} else {
			// Update is metadata-only: an accepted creative's content is frozen,
			// so only its label fields remain writable. This is the recovery path
			// for a provider-normalized name; it never recreates the creative.
			common["name"] = "string"
			common["adlabels"] = "array"
		}
	default:
		return nil
	}
	required := map[string][]string{"campaigns": {"name", "objective", "special_ad_categories"}, "adsets": {"name", "campaign_id", "billing_event", "optimization_goal", "targeting"}, "ads": {"name", "adset_id", "creative"}, "creatives": {"name"}}
	out := map[string]Field{}
	for k, v := range common {
		f := Field{Type: v}
		if k == "status" {
			f.Enum = []string{"ACTIVE", "PAUSED"}
			if operation == "update" {
				f.Enum = append(f.Enum, "ARCHIVED", "DELETED")
			}
		}
		if k == "objective" {
			f.Enum = []string{"OUTCOME_AWARENESS", "OUTCOME_ENGAGEMENT", "OUTCOME_LEADS", "OUTCOME_SALES", "OUTCOME_TRAFFIC", "OUTCOME_APP_PROMOTION"}
		}
		out[k] = f
	}
	if operation == "create" {
		for _, k := range required[resource] {
			f := out[k]
			f.Required = true
			out[k] = f
		}
	}
	return out
}
func Validate(resource, operation string, p map[string]any) error {
	fields := Fields(resource, operation)
	if fields == nil || (operation != "create" && operation != "update") {
		return output.Fail("SCHEMA_UNSUPPORTED", "Unsupported mutation schema.", 2)
	}
	if len(p) == 0 {
		return output.Fail("VALIDATION", "Payload must contain fields.", 5)
	}
	for k, v := range p {
		f, ok := fields[k]
		if !ok {
			return output.Fail("VALIDATION", "Unsupported writable field: "+k, 5)
		}
		valid := false
		switch f.Type {
		case "string":
			s, ok := v.(string)
			valid = ok && s != ""
		case "id":
			valid = config.ID(fmt.Sprint(v)) == nil
		case "integer":
			s := fmt.Sprint(v)
			valid = s != ""
			for _, c := range s {
				if c < '0' || c > '9' {
					valid = false
				}
			}
		case "object":
			_, valid = v.(map[string]any)
		case "array":
			_, valid = v.([]any)
		case "boolean":
			_, valid = v.(bool)
		}
		if len(f.Enum) > 0 {
			allowed := false
			for _, value := range f.Enum {
				if v == value {
					allowed = true
				}
			}
			valid = valid && allowed
		}
		if !valid {
			return output.Fail("VALIDATION", fmt.Sprintf("%s requires %s.", k, f.Type), 5)
		}
	}
	for k, f := range fields {
		if f.Required && p[k] == nil {
			return output.Fail("VALIDATION", "Required create field: "+k, 5)
		}
	}
	if s, ok := p["status"].(string); ok {
		allowed := s == "ACTIVE" || s == "PAUSED" || (operation == "update" && (s == "ARCHIVED" || s == "DELETED"))
		if !allowed {
			return output.Fail("VALIDATION", "Unsupported status.", 5)
		}
	}
	if p["daily_budget"] != nil && p["lifetime_budget"] != nil {
		return output.Fail("VALIDATION", "Choose one budget type.", 5)
	}
	if obj, ok := p["creative"].(map[string]any); ok {
		if len(obj) != 1 || config.ID(fmt.Sprint(obj["creative_id"])) != nil {
			return output.Fail("VALIDATION", "creative must reference exactly one existing creative_id.", 5)
		}
	}
	if operation == "create" && resource == "creatives" && p["object_story_spec"] == nil && p["object_story_id"] == nil && p["asset_feed_spec"] == nil {
		return output.Fail("VALIDATION", "Creative requires object_story_spec, object_story_id or asset_feed_spec.", 5)
	}
	return nil
}
func Keys(p map[string]any) string {
	keys := []string{"id", "account_id"}
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
func Defaults(resource string, p map[string]any) {
	if resource != "creatives" && p["status"] == nil {
		p["status"] = "PAUSED"
	}
}
func Decode(b []byte) (map[string]any, error) {
	var p map[string]any
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	if err := d.Decode(&p); err != nil || p == nil {
		return nil, output.Fail("VALIDATION", "Expected a native JSON object.", 5)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, output.Fail("VALIDATION", "Only one JSON value is allowed.", 5)
	}
	return p, nil
}
