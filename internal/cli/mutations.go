package cli

import (
	"fmt"
	"net/url"
	"strings"

	"adsctl/internal/config"
	"adsctl/internal/output"
	"adsctl/internal/platform/meta/graph"
	"adsctl/internal/platform/meta/schema"
	"github.com/spf13/cobra"
)

type mutationFlags struct {
	File, Name, Objective, Categories, Adset, Creative, Status string
	Dry                                                        bool
}

func (a *App) addMutations(g *cobra.Command, resource string) {
	for _, op := range []string{"create", "update", "validate", "pause", "activate", "archive", "delete"} {
		if resource == "creatives" && (op == "pause" || op == "activate" || op == "archive") {
			continue
		}
		f := &mutationFlags{}
		operation, id := "create", ""
		use := op
		if op != "create" && op != "validate" {
			use += " ID"
		}
		effect := "write"
		if op == "validate" {
			effect = "read"
		}
		cmd := action(use, op+" "+resource+" with explicit account scope", effect, nil)
		if op != "create" && op != "validate" {
			cmd.Args = cobra.ExactArgs(1)
		}
		cmd.Flags().BoolVar(&f.Dry, "dry-run", false, "Return plan without advertising writes")
		if op == "create" || op == "update" || op == "validate" {
			cmd.Flags().StringVar(&f.File, "file", "", "Native JSON payload path or - for stdin")
			if op != "validate" {
				cmd.Flags().StringVar(&f.Name, "name", "", "Native name")
				if resource != "creatives" {
					cmd.Flags().StringVar(&f.Status, "status", "", "Native status (create defaults PAUSED)")
				}
				if resource == "campaigns" {
					cmd.Flags().StringVar(&f.Objective, "objective", "", "Native campaign objective")
					cmd.Flags().StringVar(&f.Categories, "special-ad-categories", "", "Comma-separated categories or NONE")
				}
				if resource == "ads" {
					cmd.Flags().StringVar(&f.Creative, "creative", "", "Existing creative ID")
					if op == "create" {
						cmd.Flags().StringVar(&f.Adset, "adset", "", "Existing ad set ID")
					}
				}
			} else {
				cmd.Flags().StringVar(&operation, "operation", "create", "create or update")
				cmd.Flags().StringVar(&id, "id", "", "Existing object ID for update validation")
			}
		}
		cmd.RunE = func(c *cobra.Command, args []string) error {
			actual := op
			objID := ""
			if len(args) > 0 {
				objID = args[0]
			}
			if op == "validate" {
				actual = operation
				objID = id
			}
			write := op != "validate"
			r, err := a.runtime(c, true, write)
			if err != nil {
				return err
			}
			defer r.Cancel()
			var payload map[string]any
			switch actual {
			case "create", "update":
				payload, err = a.payload(c, resource, actual, f)
			case "pause":
				payload = map[string]any{"status": "PAUSED"}
			case "activate":
				payload = map[string]any{"status": "ACTIVE"}
			case "archive":
				payload = map[string]any{"status": "ARCHIVED"}
			case "delete":
				payload = map[string]any{}
			default:
				return output.Fail("USAGE", "Operation must be create or update.", 2)
			}
			if err != nil {
				return err
			}
			if actual != "create" {
				if err = verify(r, resource, objID); err != nil {
					return err
				}
			}
			if err = verifyParents(r, resource, payload); err != nil {
				return err
			}
			if op == "validate" {
				return a.emit(c, map[string]any{"local_valid": true, "ownership_checked": true, "remote_validation": "not performed; conditional requirements are validated by Meta on mutation", "payload": payload})
			}
			receipt, err := mutate(r, resource, actual, objID, payload, f.Dry)
			if err != nil {
				return a.partial(c, receipt, err)
			}
			return a.emit(c, receipt)
		}
		g.AddCommand(cmd)
	}
	if resource == "campaigns" || resource == "adsets" {
		a.addBudget(g, resource)
	}
	if resource == "ads" {
		g2 := group("creative", "Change an ad's creative reference")
		g.AddCommand(g2)
		var creative string
		var dry bool
		cmd := action("set ID", "Reference an existing creative", "write", func(c *cobra.Command, args []string) error {
			r, err := a.runtime(c, true, true)
			if err != nil {
				return err
			}
			defer r.Cancel()
			if err = verify(r, "ads", args[0]); err != nil {
				return err
			}
			if err = verify(r, "creatives", creative); err != nil {
				return err
			}
			receipt, err := mutate(r, "ads", "update", args[0], map[string]any{"creative": map[string]any{"creative_id": creative}}, dry)
			if err != nil {
				return a.partial(c, receipt, err)
			}
			return a.emit(c, receipt)
		})
		cmd.Args = cobra.ExactArgs(1)
		cmd.Flags().StringVar(&creative, "creative", "", "Creative ID")
		cmd.Flags().BoolVar(&dry, "dry-run", false, "Plan without writing")
		g2.AddCommand(cmd)
	}
	if resource == "ads" || resource == "creatives" {
		a.addPreview(g, resource)
	}
}
func (a *App) payload(c *cobra.Command, resource, op string, f *mutationFlags) (map[string]any, error) {
	p := map[string]any{}
	if f.File != "" {
		for _, flag := range []string{"name", "objective", "special-ad-categories", "adset", "creative", "status"} {
			if c.Flags().Changed(flag) {
				return nil, output.Fail("USAGE", "File payload cannot be combined with scalar write flags.", 2)
			}
		}
		b, err := a.readFile(f.File)
		if err != nil {
			return nil, err
		}
		p, err = schema.Decode(b)
		if err != nil {
			return nil, err
		}
	} else {
		if f.Name != "" {
			p["name"] = f.Name
		}
		if f.Objective != "" {
			p["objective"] = f.Objective
		}
		if f.Status != "" {
			p["status"] = f.Status
		}
		if f.Categories != "" {
			cats := []any{}
			if f.Categories != "NONE" {
				for _, s := range strings.Split(f.Categories, ",") {
					cats = append(cats, s)
				}
			}
			p["special_ad_categories"] = cats
		}
		if f.Adset != "" {
			p["adset_id"] = f.Adset
		}
		if f.Creative != "" {
			p["creative"] = map[string]any{"creative_id": f.Creative}
		}
	}
	if op == "create" {
		schema.Defaults(resource, p)
	}
	return p, schema.Validate(resource, op, p)
}
func verifyParents(r *runtime, resource string, p map[string]any) error {
	for field, parent := range map[string]string{"campaign_id": "campaigns", "adset_id": "adsets"} {
		if id, ok := p[field]; ok {
			if err := verify(r, parent, fmt.Sprint(id)); err != nil {
				return err
			}
		}
	}
	if obj, ok := p["creative"].(map[string]any); ok {
		return verify(r, "creatives", fmt.Sprint(obj["creative_id"]))
	}
	return nil
}
func mutate(r *runtime, resource, op, id string, p map[string]any, dry bool) (map[string]any, error) {
	method, path := "POST", id
	if op == "create" {
		path = r.Account + "/" + edges[resource]
	}
	if op == "delete" {
		method = "DELETE"
	}
	receipt := map[string]any{"operation": op, "resource": resource, "id": id, "payload": p, "dry_run": dry, "state": "planned"}
	if dry {
		return receipt, nil
	}
	params, err := graph.Form(p)
	if err != nil {
		return nil, err
	}
	v, err := r.Client.Request(r.Ctx, method, path, params)
	if err != nil {
		receipt["state"] = "failed"
		if output.AsError(err).Exit == 10 {
			receipt["state"] = "unknown"
		}
		return receipt, err
	}
	receipt["response"] = v.Value
	if op == "create" {
		id = fmt.Sprint(v.Value["id"])
		if config.ID(id) != nil {
			receipt["state"] = "unknown"
			return receipt, output.Fail("MUTATION_UNKNOWN", "Create returned no valid ID; reconcile before retrying.", 10)
		}
		receipt["id"] = id
	} else if v.Value["success"] != true {
		receipt["state"] = "unknown"
		return receipt, output.Fail("MUTATION_UNKNOWN", "Mutation did not acknowledge success; reconcile before retrying.", 10)
	}
	receipt["state"] = "applied"
	if op == "delete" {
		receipt["verification"] = "provider acknowledged deletion"
		return receipt, nil
	}
	fields := schema.Keys(p)
	if resource != "creatives" {
		fields = joinFields(fields, "status,effective_status")
	}
	observed, err := r.Client.Request(r.Ctx, "GET", id, url.Values{"fields": {fields}})
	if err != nil {
		cause := output.AsError(err)
		receipt["state"] = "applied_unverified"
		receipt["verification_error"] = cause
		outErr := output.Fail("APPLIED_UNVERIFIED", "Write acknowledged but verification read failed; do not recreate.", 9)
		outErr.HTTPStatus = cause.HTTPStatus
		outErr.Provider = cause.Provider
		return receipt, outErr
	}
	receipt["observed"] = observed.Value
	if fmt.Sprint(observed.Value["account_id"]) != strings.TrimPrefix(r.Account, "act_") {
		receipt["state"] = "applied_unverified"
		return receipt, output.Fail("APPLIED_UNVERIFIED", "Could not confirm resource account after write.", 9)
	}
	if status, ok := p["status"]; ok && observed.Value["status"] != status {
		receipt["state"] = "applied_unverified"
		return receipt, output.Fail("APPLIED_UNVERIFIED", "Configured status has not converged to the requested value.", 9)
	}
	if path := schema.FirstMismatch(resource, p, observed.Value); path != "" {
		receipt["state"] = "applied_unverified"
		return receipt, output.Fail("APPLIED_UNVERIFIED", "Observed field "+path+" has not converged to the requested value; do not recreate.", 9)
	}
	receipt["state"] = "verified"
	receipt["verification"] = "read after write; observed fields returned for comparison"
	return receipt, nil
}
func (a *App) addBudget(g *cobra.Command, resource string) {
	bg := group("budget", "Set budget without changing ownership")
	g.AddCommand(bg)
	var daily, lifetime, currency string
	var dry bool
	cmd := action("set ID", "Set exactly one native minor-unit budget", "write", func(c *cobra.Command, args []string) error {
		if (daily == "") == (lifetime == "") || currency == "" {
			return output.Fail("USAGE", "Require one budget and explicit --currency.", 2)
		}
		r, err := a.runtime(c, true, true)
		if err != nil {
			return err
		}
		defer r.Cancel()
		if err = verify(r, resource, args[0]); err != nil {
			return err
		}
		account, err := r.Client.Request(r.Ctx, "GET", r.Account, url.Values{"fields": {"id,currency"}})
		if err != nil {
			return err
		}
		if account.Value["currency"] != currency {
			return output.Fail("CURRENCY_MISMATCH", "Budget currency differs from account currency.", 5)
		}
		obj, err := r.Client.Request(r.Ctx, "GET", args[0], url.Values{"fields": {"id,daily_budget,lifetime_budget" + map[bool]string{true: ",campaign_id"}[resource == "adsets"]}})
		if err != nil {
			return err
		}
		if resource == "adsets" {
			parent := fmt.Sprint(obj.Value["campaign_id"])
			if err = verify(r, "campaigns", parent); err != nil {
				return err
			}
			campaign, err := r.Client.Request(r.Ctx, "GET", parent, url.Values{"fields": {"id,daily_budget,lifetime_budget"}})
			if err != nil {
				return err
			}
			if positive(campaign.Value["daily_budget"]) || positive(campaign.Value["lifetime_budget"]) {
				return output.Fail("BUDGET_OWNERSHIP", "Campaign owns the budget; change the campaign instead.", 5)
			}
		} else if !positive(obj.Value["daily_budget"]) && !positive(obj.Value["lifetime_budget"]) {
			return output.Fail("BUDGET_OWNERSHIP", "Campaign does not currently own a budget.", 5)
		}
		field, value := "daily_budget", daily
		other := "lifetime_budget"
		if lifetime != "" {
			field, value = "lifetime_budget", lifetime
			other = "daily_budget"
		}
		if positive(obj.Value[other]) {
			return output.Fail("BUDGET_TYPE", "Changing budget type requires a reviewed native update.", 5)
		}
		p := map[string]any{field: value}
		if err = schema.Validate(resource, "update", p); err != nil {
			return err
		}
		if !positive(value) {
			return output.Fail("VALIDATION", "Budget must be positive.", 5)
		}
		receipt, err := mutate(r, resource, "update", args[0], p, dry)
		if err != nil {
			return a.partial(c, receipt, err)
		}
		return a.emit(c, receipt)
	})
	cmd.Args = cobra.ExactArgs(1)
	cmd.Flags().StringVar(&daily, "daily-budget-minor", "", "Daily budget in native minor units")
	cmd.Flags().StringVar(&lifetime, "lifetime-budget-minor", "", "Lifetime budget in native minor units")
	cmd.Flags().StringVar(&currency, "currency", "", "Expected account currency")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "Plan without writing")
	bg.AddCommand(cmd)
}
func positive(v any) bool { s := fmt.Sprint(v); return s != "" && s != "0" && s != "<nil>" }
func (a *App) addPreview(g *cobra.Command, resource string) {
	var format string
	cmd := action("preview ID", "Return provider preview HTML without executing it", "read", func(c *cobra.Command, args []string) error {
		r, err := a.runtime(c, true, false)
		if err != nil {
			return err
		}
		defer r.Cancel()
		if err = verify(r, resource, args[0]); err != nil {
			return err
		}
		return a.list(c, r, args[0]+"/previews", url.Values{"ad_format": {format}}, graph.PageOptions{All: true, PageSize: 100, MaxItems: 100})
	})
	cmd.Args = cobra.ExactArgs(1)
	cmd.Flags().StringVar(&format, "format", "DESKTOP_FEED_STANDARD", "Native ad preview format")
	g.AddCommand(cmd)
}
