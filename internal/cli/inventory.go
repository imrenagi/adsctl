package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"adsctl/internal/output"
	"adsctl/internal/platform/meta/graph"
	"adsctl/internal/state"
	"github.com/spf13/cobra"
)

type snapshot struct {
	Version    int              `json:"version"`
	Account    string           `json:"account_id"`
	APIVersion string           `json:"api_version"`
	At         string           `json:"fetched_at"`
	Complete   bool             `json:"complete"`
	Resources  map[string][]any `json:"resources"`
}

func (a *App) addInventory(meta *cobra.Command) {
	for _, op := range []string{"activity", "recommendations", "issues"} {
		g := group(op, "Inspect account "+op)
		meta.AddCommand(g)
		var since, until, fields string
		var bound int
		cmd := action("list", "Read bounded "+op+" evidence", "read", func(c *cobra.Command, _ []string) error {
			r, err := a.runtime(c, true, false)
			if err != nil {
				return err
			}
			defer r.Cancel()
			if op == "issues" {
				data, err := collectIssues(r, bound)
				if err != nil {
					return a.partial(c, data, err)
				}
				return a.emit(c, data)
			}
			q := url.Values{"fields": {fields}}
			if op == "activity" {
				s, e1 := time.Parse(time.RFC3339, since)
				e, e2 := time.Parse(time.RFC3339, until)
				if e1 != nil || e2 != nil || e.Before(s) {
					return output.Fail("USAGE", "Activity requires ordered --since and --until RFC3339 timestamps.", 2)
				}
				q.Set("since", s.Format(time.RFC3339))
				q.Set("until", e.Format(time.RFC3339))
			}
			edge := map[string]string{"activity": "activities", "recommendations": "recommendations"}[op]
			return a.list(c, r, r.Account+"/"+edge, q, graph.PageOptions{All: true, MaxItems: bound, PageSize: 100})
		})
		if op == "activity" {
			cmd.Flags().StringVar(&since, "since", "", "Start timestamp")
			cmd.Flags().StringVar(&until, "until", "", "End timestamp")
			fields = "event_time,event_type,object_id,object_name,actor_id,actor_name,extra_data"
		} else {
			fields = "recommendations"
		}
		if op != "issues" {
			cmd.Flags().StringVar(&fields, "fields", fields, "Native fields")
		}
		cmd.Flags().IntVar(&bound, "max-items", 10000, "Row bound")
		g.AddCommand(cmd)
	}
	g := group("snapshots", "Save and compare account configuration inventories")
	meta.AddCommand(g)
	var save string
	var bound int
	create := action("create", "Save a bounded configuration snapshot", "read", func(c *cobra.Command, _ []string) error {
		if save == "" {
			return output.Fail("USAGE", "Snapshot requires --save.", 2)
		}
		r, err := a.runtime(c, true, false)
		if err != nil {
			return err
		}
		defer r.Cancel()
		snap := snapshot{Version: 1, Account: r.Account, APIVersion: r.Version, At: time.Now().UTC().Format(time.RFC3339), Complete: true, Resources: map[string][]any{}}
		for _, resource := range []string{"campaigns", "adsets", "ads", "creatives"} {
			fields := defaultFields(resource)
			if resource == "campaigns" || resource == "adsets" {
				fields = joinFields(fields, "daily_budget,lifetime_budget")
			}
			if resource == "adsets" {
				fields = joinFields(fields, "targeting,optimization_goal,billing_event")
			}
			result, err := r.Client.List(r.Ctx, r.Account+"/"+edges[resource], url.Values{"fields": {fields}}, graph.PageOptions{All: true, MaxItems: bound, PageSize: 100})
			snap.Resources[resource] = result.Rows
			if err != nil || !result.Complete {
				snap.Complete = false
				if err == nil {
					err = output.Fail("INCOMPLETE", "Snapshot exceeded row bound.", 9)
				}
				if saveErr := state.Exclusive(save, snap); saveErr != nil {
					return saveErr
				}
				return a.partial(c, snap, err)
			}
		}
		if err = state.Exclusive(save, snap); err != nil {
			return err
		}
		return a.emit(c, map[string]any{"file": save, "snapshot": snap})
	})
	create.Flags().StringVar(&save, "save", "", "New snapshot path")
	create.Flags().IntVar(&bound, "max-items", 10000, "Bound per resource")
	g.AddCommand(create)
	var before, after string
	diff := action("diff", "Compare complete snapshots from the same account/version", "local", func(c *cobra.Command, _ []string) error {
		var left, right snapshot
		if err := state.Read(before, &left); err != nil {
			return err
		}
		if err := state.Read(after, &right); err != nil {
			return err
		}
		data, err := diffSnapshots(left, right)
		if err != nil {
			return err
		}
		return a.emit(c, data)
	})
	diff.Flags().StringVar(&before, "before", "", "Earlier snapshot")
	diff.Flags().StringVar(&after, "after", "", "Later snapshot")
	g.AddCommand(diff)
}
func collectIssues(r *runtime, bound int) (map[string]any, error) {
	account, err := r.Client.Request(r.Ctx, "GET", r.Account, url.Values{"fields": {"id,name,account_status,disable_reason"}})
	if err != nil {
		return nil, err
	}
	result, err := r.Client.List(r.Ctx, r.Account+"/ads", url.Values{"fields": {"id,name,status,effective_status,issues_info,ad_review_feedback"}}, graph.PageOptions{All: true, MaxItems: bound, PageSize: 100})
	issues := []any{}
	for _, row := range result.Rows {
		if m, ok := row.(map[string]any); ok {
			status := fmt.Sprint(m["effective_status"])
			if status == "DISAPPROVED" || status == "WITH_ISSUES" || status == "PENDING_REVIEW" || status == "PENDING_BILLING_INFO" || hasEvidence(m["issues_info"]) || hasEvidence(m["ad_review_feedback"]) {
				issues = append(issues, m)
			}
		}
	}
	data := map[string]any{"account": account.Value, "ads": issues, "complete": result.Complete, "coverage": "Account status and available ad issue/review fields; provider field access may vary."}
	if err == nil && !result.Complete {
		err = output.Fail("INCOMPLETE", "Issue inventory exceeded row bound.", 9)
	}
	return data, err
}
func hasEvidence(v any) bool {
	switch x := v.(type) {
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	case string:
		return x != ""
	}
	return false
}
func diffSnapshots(left, right snapshot) (any, error) {
	if left.Version != 1 || right.Version != 1 || left.Account != right.Account || left.APIVersion != right.APIVersion {
		return nil, output.Fail("SNAPSHOT_SCOPE", "Snapshots must use schema 1 and the same account/API version.", 2)
	}
	if !left.Complete || !right.Complete {
		return nil, output.Fail("INCOMPLETE", "Cannot infer deletions from incomplete inventories.", 9)
	}
	changes := []any{}
	for _, resource := range []string{"campaigns", "adsets", "ads", "creatives"} {
		l, lok := left.Resources[resource]
		rr, rok := right.Resources[resource]
		if !lok || !rok {
			return nil, output.Fail("INCOMPLETE", "Snapshot is missing a required resource inventory.", 9)
		}
		old := indexRows(l)
		newRows := indexRows(rr)
		for id, v := range old {
			next, ok := newRows[id]
			if !ok {
				changes = append(changes, map[string]any{"resource": resource, "id": id, "change": "removed", "before": v})
				continue
			}
			a, _ := json.Marshal(v)
			b, _ := json.Marshal(next)
			if string(a) != string(b) {
				changes = append(changes, map[string]any{"resource": resource, "id": id, "change": "changed", "before": v, "after": next})
			}
		}
		for id, v := range newRows {
			if _, ok := old[id]; !ok {
				changes = append(changes, map[string]any{"resource": resource, "id": id, "change": "added", "after": v})
			}
		}
	}
	return changes, nil
}
func indexRows(rows []any) map[string]any {
	out := map[string]any{}
	for _, v := range rows {
		if m, ok := v.(map[string]any); ok {
			out[fmt.Sprint(m["id"])] = m
		}
	}
	return out
}
