package cli

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"adsctl/internal/config"
	"adsctl/internal/launch"
	"adsctl/internal/output"
	"adsctl/internal/platform/meta/graph"
	"adsctl/internal/platform/meta/schema"
	"adsctl/internal/state"
	"github.com/spf13/cobra"
)

func (a *App) addLaunch(meta *cobra.Command) {
	g := group("launch", "Plan, create paused resources and recover checkpointed runs")
	meta.AddCommand(g)
	var dir string
	g.PersistentFlags().StringVar(&dir, "state-dir", "", "Private persistent run directory")
	a.addLaunchTemplate(g)
	for _, op := range []string{"validate", "plan"} {
		var file, save string
		cmd := action(op, "Compile and inspect a launch without creating resources", "read", func(c *cobra.Command, _ []string) error {
			if file == "-" {
				return output.Fail("USAGE", "Launch manifests require a file path for relative media references.", 2)
			}
			b, err := a.readFile(file)
			if err != nil {
				return err
			}
			r, err := a.runtime(c, true, false)
			if err != nil {
				return err
			}
			defer r.Cancel()
			scope, err := launchScope(r)
			if err != nil {
				return err
			}
			plan, err := launch.Compile(b, filepath.Dir(file), scope)
			if err != nil {
				return err
			}
			if err = preflightPlan(r, plan); err != nil {
				return err
			}
			if save != "" {
				if err = state.Exclusive(save, plan); err != nil {
					return err
				}
			}
			return a.emit(c, plan)
		})
		cmd.Flags().StringVar(&file, "file", "", "Launch manifest path")
		if op == "plan" {
			cmd.Flags().StringVar(&save, "save", "", "New plan JSON file")
		}
		g.AddCommand(cmd)
	}
	var planFile string
	var dry bool
	apply := action("apply", "Apply a reviewed plan, creating paused resources", "write", func(c *cobra.Command, _ []string) error {
		var plan launch.Plan
		if err := state.Read(planFile, &plan); err != nil {
			return err
		}
		if err := plan.Validate(true); err != nil {
			return err
		}
		r, err := a.runtime(c, true, true)
		if err != nil {
			return err
		}
		defer r.Cancel()
		if err = matchPlan(r, plan); err != nil {
			return err
		}
		if err = preflightPlan(r, plan); err != nil {
			return err
		}
		if dry {
			return a.emit(c, map[string]any{"dry_run": true, "plan": plan})
		}
		rootDir, err := state.Dir(dir)
		if err != nil {
			return err
		}
		planKey := "plan_" + plan.Digest
		releasePlan, err := state.Lock(rootDir, planKey)
		if err != nil {
			return err
		}
		defer releasePlan()
		indexPath, _ := state.Path(rootDir, planKey)
		if _, statErr := os.Stat(indexPath); statErr == nil {
			var index map[string]string
			if err = state.Read(indexPath, &index); err != nil {
				return err
			}
			a.meta["run_id"] = index["run_id"]
			return output.Fail("RUN_EXISTS", "This plan already has a local run; inspect or resume that run.", 2)
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		run, err := launch.NewRun(plan)
		if err != nil {
			return err
		}
		unlock, err := state.Lock(rootDir, run.ID)
		if err != nil {
			return err
		}
		defer unlock()
		if err = saveRun(rootDir, &run); err != nil {
			return err
		}
		if err = state.Save(rootDir, planKey, map[string]string{"run_id": run.ID}); err != nil {
			return err
		}
		a.meta["run_id"] = run.ID
		err = a.executeRun(r, rootDir, &run)
		if err != nil {
			return a.partial(c, run, err)
		}
		return a.emit(c, run)
	})
	apply.Flags().StringVar(&planFile, "plan", "", "Reviewed plan JSON")
	apply.Flags().BoolVar(&dry, "dry-run", false, "Revalidate without creating a run or resources")
	g.AddCommand(apply)
	for _, op := range []string{"status", "resume", "activate", "reconcile"} {
		var step, id string
		var retry, dry bool
		effect := "write"
		if op == "status" {
			effect = "local"
		}
		cmd := action(op+" RUN_ID", "Inspect or continue a checkpointed launch run", effect, func(c *cobra.Command, args []string) error {
			rootDir, err := state.Dir(dir)
			if err != nil {
				return err
			}
			path, err := state.Path(rootDir, args[0])
			if err != nil {
				return err
			}
			if op == "status" {
				var run launch.Run
				if err = state.Read(path, &run); err != nil {
					return err
				}
				return a.emit(c, run)
			}
			unlock, err := state.Lock(rootDir, args[0])
			if err != nil {
				return err
			}
			defer unlock()
			var run launch.Run
			if err = state.Read(path, &run); err != nil {
				return err
			}
			if err = run.Validate(); err != nil {
				return err
			}
			if run.ID != args[0] {
				return output.Fail("JOURNAL_INVALID", "Run ID does not match filename.", 2)
			}
			r, err := a.runtime(c, true, true)
			if err != nil {
				return err
			}
			defer r.Cancel()
			a.meta["run_id"] = run.ID
			if err = matchPlan(r, run.Plan); err != nil {
				return err
			}
			switch op {
			case "resume":
				if err = run.Plan.CheckFiles(); err == nil {
					err = a.executeRun(r, rootDir, &run)
				}
			case "activate":
				a.meta["dry_run"] = dry
				err = activateRun(r, rootDir, &run, dry)
			case "reconcile":
				err = reconcileRun(r, rootDir, &run, step, id, retry)
			}
			if err != nil {
				return a.partial(c, run, err)
			}
			return a.emit(c, run)
		})
		cmd.Args = cobra.ExactArgs(1)
		if op == "reconcile" {
			cmd.Flags().StringVar(&step, "step", "", "Exact step key")
			cmd.Flags().StringVar(&id, "remote-id", "", "Known remote ID or image hash")
			cmd.Flags().BoolVar(&retry, "retry-unknown", false, "Record an explicit retry decision; may create a duplicate")
		}
		if op == "activate" {
			cmd.Flags().BoolVar(&dry, "dry-run", false, "Verify scope and return activation order without writing")
		}
		g.AddCommand(cmd)
	}
	runs := group("runs", "Inspect local launch journals without credentials")
	meta.AddCommand(runs)
	var runDir string
	runs.PersistentFlags().StringVar(&runDir, "state-dir", "", "Run directory")
	get := action("get RUN_ID", "Read a local launch journal", "local", func(c *cobra.Command, args []string) error {
		root, err := state.Dir(runDir)
		if err != nil {
			return err
		}
		path, err := state.Path(root, args[0])
		if err != nil {
			return err
		}
		var run launch.Run
		if err = state.Read(path, &run); err != nil {
			return err
		}
		return a.emit(c, run)
	})
	get.Args = cobra.ExactArgs(1)
	runs.AddCommand(get)
	runs.AddCommand(action("list", "List local run receipts", "local", func(c *cobra.Command, _ []string) error {
		root, err := state.Dir(runDir)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			return a.emit(c, []any{})
		}
		if err != nil {
			return err
		}
		rows := []any{}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "run_") && strings.HasSuffix(entry.Name(), ".json") {
				var run launch.Run
				if err = state.Read(filepath.Join(root, entry.Name()), &run); err != nil {
					return err
				}
				rows = append(rows, map[string]any{"id": run.ID, "state": run.State, "name": run.Plan.Name, "scope": run.Plan.Scope, "updated_at": run.UpdatedAt})
			}
		}
		return a.emit(c, rows)
	}))
}
func launchScope(r *runtime) (launch.Scope, error) {
	response, err := r.Client.Request(r.Ctx, "GET", r.Account, url.Values{"fields": {"id,currency"}})
	if err != nil {
		return launch.Scope{}, err
	}
	currency, ok := response.Value["currency"].(string)
	if !ok || currency == "" {
		return launch.Scope{}, output.Fail("CURRENCY_UNAVAILABLE", "Account currency is required for a launch plan.", 8)
	}
	return launch.Scope{Profile: r.Profile, Account: r.Account, APIVersion: r.Version, Currency: currency}, nil
}
func matchPlan(r *runtime, p launch.Plan) error {
	scope, err := launchScope(r)
	if err != nil {
		return err
	}
	if p.Scope != scope {
		return output.Fail("PLAN_SCOPE", "Profile, account, API version or currency differs from reviewed plan.", 4)
	}
	return nil
}
func preflightPlan(r *runtime, p launch.Plan) error {
	if err := p.CheckFiles(); err != nil {
		return err
	}
	for _, s := range p.Steps {
		if s.Resource == "images" || s.Resource == "videos" {
			continue
		}
		// Only native existing references can be checked before dependencies exist.
		if err := verifyParents(r, s.Resource, s.Node.Payload); err != nil {
			return err
		}
		if s.Resource == "creatives" {
			if story, ok := s.Node.Payload["object_story_spec"].(map[string]any); ok {
				page := fmt.Sprint(story["page_id"])
				if err := config.ID(page); err != nil {
					return output.Fail("VALIDATION", "Creative object_story_spec requires a numeric page_id.", 5)
				}
				if _, err := r.Client.Request(r.Ctx, "GET", page, url.Values{"fields": {"id"}}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func saveRun(dir string, r *launch.Run) error { r.Touch(); return state.Save(dir, r.ID, r) }
func (a *App) executeRun(r *runtime, dir string, run *launch.Run) error {
	if run.State == "active" {
		return nil
	}
	// Scan all checkpoints before creating anything on a resume.
	for key, cp := range run.Steps {
		step, exists := run.Plan.Step(key)
		if exists && step.Resource == "videos" {
			checkpoint, _ := state.Path(dir, videoStepID(run.ID, key))
			if _, e := os.Stat(checkpoint); e == nil {
				var video graph.VideoCheckpoint
				if err := state.Read(checkpoint, &video); err != nil {
					return err
				}
				if video.Phase != "starting" {
					continue
				}
			}
		}
		if cp.State == "in_flight" || cp.State == "unknown" {
			cp.State = "unknown"
			run.Steps[key] = cp
			run.State = "needs_reconciliation"
			if err := saveRun(dir, run); err != nil {
				return err
			}
			return output.Fail("MUTATION_UNKNOWN", "A previous create may have succeeded; reconcile the step before resuming.", 10)
		}
	}
	for _, s := range run.Plan.Steps {
		cp := run.Steps[s.Key]
		if cp.ID != "" {
			if err := verifyStep(r, s, cp.ID, run.IDs()); err != nil {
				run.State = "verification_blocked"
				_ = saveRun(dir, run)
				return err
			}
			cp.State = "verified"
			cp.Error = nil
			run.Steps[s.Key] = cp
			if err := saveRun(dir, run); err != nil {
				return err
			}
		}
	}
	for _, s := range run.Plan.Steps {
		cp := run.Steps[s.Key]
		if cp.State == "verified" {
			continue
		}
		payload := map[string]any{}
		var err error
		if s.Resource != "images" && s.Resource != "videos" {
			payload, err = launch.Payload(s, run.IDs())
			if err != nil {
				return err
			}
			if err = verifyParents(r, s.Resource, payload); err != nil {
				return err
			}
		}
		cp.State = "in_flight"
		cp.Error = nil
		run.Steps[s.Key] = cp
		run.State = "applying"
		if err = saveRun(dir, run); err != nil {
			return err
		}
		if s.Resource == "images" || s.Resource == "videos" {
			var response graph.Response
			if s.Resource == "videos" {
				id := videoStepID(run.ID, s.Key)
				checkpoint, _ := state.Path(dir, id)
				_, statErr := os.Stat(checkpoint)
				response, err = r.Client.UploadVideo(r.Ctx, r.Account, s.File, graph.VideoUploadOptions{Profile: r.Profile, StateDir: dir, ID: id, Resume: !os.IsNotExist(statErr)})
			} else {
				response, err = r.Client.Upload(r.Ctx, r.Account, s.Resource, s.File)
			}
			cp.Receipt = response.Value
			if err == nil {
				if s.Resource == "videos" {
					cp.ID = fmt.Sprint(response.Value["id"])
					if config.ID(cp.ID) != nil {
						cp.ID = ""
					}
				} else {
					images, _ := response.Value["images"].(map[string]any)
					for _, value := range images {
						if image, ok := value.(map[string]any); ok {
							cp.ID, _ = image["hash"].(string)
							break
						}
					}
				}
				if cp.ID == "" {
					err = output.Fail("MUTATION_UNKNOWN", "Upload returned no usable ID or hash.", 10)
				}
			}
		} else {
			cp.Receipt, err = mutate(r, s.Resource, "create", "", payload, false)
			if cp.Receipt != nil {
				cp.ID, _ = cp.Receipt["id"].(string)
			}
		}
		if err != nil {
			cp.State = "failed"
			if output.AsError(err).Exit == 10 {
				cp.State = "unknown"
			} else if cp.ID != "" {
				cp.State = "applied_unverified"
			}
			cp.Error = output.AsError(err)
			run.Steps[s.Key] = cp
			run.State = "interrupted"
			if saveErr := saveRun(dir, run); saveErr != nil {
				return saveErr
			}
			return err
		}
		// Checkpoint the known ID before processing waits or further reads.
		cp.State = "applied_unverified"
		run.Steps[s.Key] = cp
		if err = saveRun(dir, run); err != nil {
			return err
		}
		if err = verifyStep(r, s, cp.ID, run.IDs()); err != nil {
			cp.Error = output.AsError(err)
			run.Steps[s.Key] = cp
			run.State = "interrupted"
			if saveErr := saveRun(dir, run); saveErr != nil {
				return saveErr
			}
			return err
		}
		cp.State = "verified"
		run.Steps[s.Key] = cp
		if err = saveRun(dir, run); err != nil {
			return err
		}
	}
	run.State = "created_paused"
	return saveRun(dir, run)
}
func verifyStep(r *runtime, s launch.Step, id string, ids map[string]string) error {
	if s.Resource == "images" {
		result, err := r.Client.List(r.Ctx, r.Account+"/adimages", url.Values{"fields": {"hash,name"}}, graph.PageOptions{All: true, PageSize: 100, MaxItems: 10000})
		if err != nil {
			return err
		}
		for _, v := range result.Rows {
			if image, ok := v.(map[string]any); ok && image["hash"] == id {
				return nil
			}
		}
		if !result.Complete {
			return output.Fail("INCOMPLETE", "Cannot verify image hash within row bound.", 9)
		}
		return output.Fail("ASSET_MISMATCH", "Image hash is absent from selected account.", 4)
	}
	if s.Resource == "videos" {
		if err := verifyVideo(r, id); err != nil {
			return err
		}
		video, err := r.Client.Request(r.Ctx, "GET", id, url.Values{"fields": {"id,status"}})
		if err != nil {
			return err
		}
		status, _ := video.Value["status"].(map[string]any)
		if status["video_status"] != "ready" {
			return output.Fail("VIDEO_PENDING", "Video is not ready; wait then resume this run.", 9)
		}
		return nil
	}
	if err := verify(r, s.Resource, id); err != nil {
		return err
	}
	expected, err := launch.Payload(s, ids)
	if err != nil {
		return err
	}
	observed, err := r.Client.Request(r.Ctx, "GET", id, url.Values{"fields": {verificationFields(s.Resource, expected)}})
	if err != nil {
		return err
	}
	if path := schema.FirstMismatch(s.Resource, expected, observed.Value); path != "" {
		return output.Fail("RESOURCE_DRIFT", "Remote field "+path+" differs from the reviewed plan; review before continuing.", 9)
	}
	return nil
}
func verificationFields(resource string, p map[string]any) string {
	return joinFields(schema.Keys(p), map[string]string{"campaigns": "objective", "adsets": "campaign_id", "ads": "adset_id", "creatives": "object_story_spec"}[resource])
}
func reconcileRun(r *runtime, dir string, run *launch.Run, key, id string, retry bool) error {
	s, ok := run.Plan.Step(key)
	if !ok {
		return output.Fail("USAGE", "Specify a valid --step.", 2)
	}
	cp := run.Steps[key]
	if cp.State != "unknown" && cp.State != "in_flight" {
		return output.Fail("USAGE", "Reconcile only an unknown or in-flight create.", 2)
	}
	if (id == "") == !retry {
		return output.Fail("USAGE", "Choose --remote-id or --retry-unknown.", 2)
	}
	var videoRetry *graph.VideoCheckpoint
	uploadKey := videoStepID(run.ID, key)
	if retry && s.Resource == "videos" {
		path, _ := state.Path(dir, uploadKey)
		if _, err := os.Stat(path); err == nil {
			var video graph.VideoCheckpoint
			if err = state.Read(path, &video); err != nil {
				return err
			}
			if video.Phase != "starting" {
				return output.Fail("USAGE", "This video has a checkpointed session; use launch resume to recover it instead of creating another asset.", 2)
			}
			videoRetry = &video
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if retry {
		cp.State = "pending"
		cp.ID = ""
		cp.Error = nil
		cp.Receipt = map[string]any{"operator_decision": "retry_unknown", "duplicate_risk": true}
		if videoRetry != nil {
			cp.Receipt["previous_upload"] = *videoRetry
		}
	}
	if id != "" {
		if err := verifyStep(r, s, id, run.IDs()); err != nil {
			return err
		}
		cp.ID = id
		cp.State = "verified"
		cp.Error = nil
		cp.Receipt = map[string]any{"operator_decision": "attach_verified_remote_object", "id": id}
	}
	run.Decisions = append(run.Decisions, map[string]any{"step": key, "remote_id": id, "retry_unknown": retry, "duplicate_risk": retry})
	run.Steps[key] = cp
	run.State = "reconciled"
	// Persist the explicit duplicate-risk decision before permitting a new start.
	if err := saveRun(dir, run); err != nil {
		return err
	}
	if videoRetry != nil {
		videoRetry.Phase, videoRetry.SessionID, videoRetry.VideoID = "new", "", ""
		videoRetry.Start, videoRetry.End = 0, 0
		return state.Save(dir, uploadKey, videoRetry)
	}
	return nil
}
func activateRun(r *runtime, dir string, run *launch.Run, dry bool) error {
	if !run.Ready() {
		return output.Fail("INCOMPLETE", "All create steps must be verified before activation.", 9)
	}
	order := []launch.Step{}
	for _, resource := range []string{"ads", "adsets", "campaigns"} {
		for _, s := range run.Plan.Steps {
			if s.Resource == resource {
				order = append(order, s)
			}
		}
	}
	// Verify all targets and referenced parents before activating any child.
	for _, s := range order {
		id := run.Steps[s.Key].ID
		if err := verify(r, s.Resource, id); err != nil {
			return err
		}
		p, err := launch.Payload(s, run.IDs())
		if err != nil {
			return err
		}
		if err = verifyParents(r, s.Resource, p); err != nil {
			return err
		}
		observed, err := r.Client.Request(r.Ctx, "GET", id, url.Values{"fields": {joinFields(verificationFields(s.Resource, p), "status,effective_status")}})
		if err != nil {
			return err
		}
		status := observed.Value["status"]
		if status != "PAUSED" && status != "ACTIVE" {
			return output.Fail("RESOURCE_DRIFT", "Only paused or active recorded objects can be activated.", 9)
		}
		delete(p, "status")
		if path := schema.FirstMismatch(s.Resource, p, observed.Value); path != "" {
			return output.Fail("RESOURCE_DRIFT", "Run field "+path+" changed since the reviewed plan; activation blocked.", 9)
		}
		run.Activation[s.Key] = launch.Checkpoint{State: "planned", ID: id, Receipt: map[string]any{"observed": observed.Value, "scope": "Recorded objects only; existing parents retain their configured state."}}
	}
	if dry {
		for i, s := range order {
			cp := run.Activation[s.Key]
			cp.Receipt["order"] = i + 1
			cp.Receipt["requested_status"] = "ACTIVE"
			run.Activation[s.Key] = cp
		}
		return nil
	}
	for _, s := range order {
		id := run.Steps[s.Key].ID
		observed, err := r.Client.Request(r.Ctx, "GET", id, url.Values{"fields": {"id,status"}})
		if err != nil {
			return err
		}
		if observed.Value["status"] == "ACTIVE" {
			run.Activation[s.Key] = launch.Checkpoint{State: "verified", ID: id}
			if err = saveRun(dir, run); err != nil {
				return err
			}
			continue
		}
		if observed.Value["status"] != "PAUSED" {
			return output.Fail("RESOURCE_DRIFT", "Only paused or already active run resources can be activated.", 9)
		}
		run.Activation[s.Key] = launch.Checkpoint{State: "in_flight", ID: id}
		run.State = "activating"
		if err = saveRun(dir, run); err != nil {
			return err
		}
		receipt, err := mutate(r, s.Resource, "activate", id, map[string]any{"status": "ACTIVE"}, false)
		cp := launch.Checkpoint{State: "verified", ID: id, Receipt: receipt}
		if err != nil {
			cp.State = "applied_unverified"
			cp.Error = output.AsError(err)
		}
		run.Activation[s.Key] = cp
		if saveErr := saveRun(dir, run); saveErr != nil {
			return saveErr
		}
		if err != nil {
			return err
		}
	}
	run.State = "active"
	return saveRun(dir, run)
}

// Stable per-run IDs preserve a video's upload session across process crashes.
func videoStepID(runID, key string) string {
	return fmt.Sprintf("upload_%x", sha256.Sum256([]byte(runID+"|"+key)))
}
