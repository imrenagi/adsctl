package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"adsctl/internal/launch"
	"adsctl/internal/output"
	"adsctl/internal/platform/meta/graph"
	"adsctl/internal/state"
)

const launchFixture = `version: 1
kind: meta-launch
name: traffic
campaigns:
  - key: main
    payload:
      name: Traffic
      objective: OUTCOME_TRAFFIC
      special_ad_categories: []
adsets:
  - key: audience
    campaign_ref: main
    payload:
      name: Audience
      daily_budget: 2500
      billing_event: IMPRESSIONS
      optimization_goal: LINK_CLICKS
      targeting:
        geo_locations:
          countries: [ID]
        instagram_positions: [reels, story]
creatives:
  - key: post
    payload:
      name: Post
      object_story_id: '123_456'
ads:
  - key: ad
    adset_ref: audience
    creative_ref: post
    payload:
      name: Ad
`

type launchServer struct {
	mu               sync.Mutex
	objects          map[string]map[string]any
	creates          int
	activation       []string
	ambiguous        bool
	reorderPositions bool
}

func (s *launchServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/v25.0/")
	if path == "act_123" {
		_, _ = fmt.Fprint(w, `{"id":"act_123","currency":"USD"}`)
		return
	}
	if r.Method == "POST" {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(400)
			return
		}
		if strings.Contains(path, "/") {
			s.creates++
			id := fmt.Sprint(100 + s.creates)
			obj := map[string]any{"id": id, "account_id": "123"}
			for key, vs := range r.Form {
				var val any
				d := json.NewDecoder(strings.NewReader(vs[0]))
				d.UseNumber()
				if err := d.Decode(&val); err != nil || !json.Valid([]byte(vs[0])) {
					val = vs[0]
				}
				obj[key] = val
			}
			s.objects[id] = obj
			if s.ambiguous {
				s.ambiguous = false
				w.WriteHeader(500)
				_, _ = fmt.Fprint(w, `{"unavailable":true}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
			return
		}
		obj := s.objects[path]
		for key, values := range r.Form {
			obj[key] = values[0]
		}
		if obj["status"] == "ACTIVE" {
			s.activation = append(s.activation, path)
		}
		_, _ = fmt.Fprint(w, `{"success":true}`)
		return
	}
	obj := s.objects[path]
	if obj == nil {
		_, _ = fmt.Fprint(w, `{"error":{"code":100,"message":"unknown"}}`)
		return
	}
	// Simulate Meta normalizing the order of a set-valued targeting field.
	if s.reorderPositions {
		if targeting, ok := obj["targeting"].(map[string]any); ok {
			if positions, ok := targeting["instagram_positions"].([]any); ok {
				reversed := make([]any, len(positions))
				for i := range positions {
					reversed[i] = positions[len(positions)-1-i]
				}
				targeting["instagram_positions"] = reversed
			}
		}
	}
	_ = json.NewEncoder(w).Encode(obj)
}
func launchFiles(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	manifest := filepath.Join(dir, "launch.yaml")
	plan := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(manifest, []byte(launchFixture), 0600); err != nil {
		t.Fatal(err)
	}
	return manifest, plan, filepath.Join(dir, "state")
}
func TestLaunchFullFlowAndDuplicatePlanGuard(t *testing.T) {
	server := &launchServer{objects: map[string]map[string]any{}}
	a, out, _, cfg := testApp(t, server.serve)
	manifest, plan, dir := launchFiles(t)
	exec := func(args ...string) error {
		out.Reset()
		base := []string{"--config", cfg, "meta", "launch"}
		base = append(base, args...)
		base = append(base, "--profile", "test", "--account", "123", "--state-dir", dir)
		return executeTest(a, base)
	}
	if err := exec("plan", "--file", manifest, "--save", plan); err != nil {
		t.Fatal(err)
	}
	if server.creates != 0 {
		t.Fatal("plan mutated")
	}
	if err := exec("apply", "--plan", plan); err != nil {
		t.Fatal(err, out.String())
	}
	if server.creates != 4 {
		t.Fatal(server.creates)
	}
	var env struct {
		Data launch.Run `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	run := env.Data
	if run.State != "created_paused" || !run.Ready() {
		t.Fatal(run)
	}
	if err := exec("apply", "--plan", plan); err == nil || output.AsError(err).Code != "RUN_EXISTS" || server.creates != 4 {
		t.Fatal("duplicate apply", err, server.creates)
	}
	if err := exec("resume", run.ID); err != nil || server.creates != 4 {
		t.Fatal("resume duplicated", err)
	}
	if err := exec("activate", run.ID, "--dry-run"); err != nil || len(server.activation) != 0 || !bytes.Contains(out.Bytes(), []byte(`"state": "planned"`)) {
		t.Fatal(err, out.String())
	}
	if err := exec("activate", run.ID); err != nil {
		t.Fatal(err, out.String())
	}
	if strings.Join(server.activation, ",") != "104,102,101" {
		t.Fatal("activation order", server.activation)
	}
	if err := exec("activate", run.ID); err != nil || len(server.activation) != 3 {
		t.Fatal("repeat activation", err, server.activation)
	}
}
func TestLaunchResumesAfterProviderReorderedPlacements(t *testing.T) {
	server := &launchServer{objects: map[string]map[string]any{}, reorderPositions: true}
	a, out, _, cfg := testApp(t, server.serve)
	manifest, plan, dir := launchFiles(t)
	exec := func(args ...string) error {
		out.Reset()
		base := append([]string{"--config", cfg, "meta", "launch"}, args...)
		return executeTest(a, append(base, "--profile", "test", "--account", "123", "--state-dir", dir))
	}
	if err := exec("plan", "--file", manifest, "--save", plan); err != nil {
		t.Fatal(err)
	}
	if err := exec("apply", "--plan", plan); err != nil {
		t.Fatal(err, out.String())
	}
	var env struct {
		Data launch.Run `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.State != "created_paused" || !env.Data.Ready() {
		t.Fatal("apply blocked on provider reordering", env.Data.State)
	}
	// Resume re-reads every created resource; reordered placements must verify.
	if err := exec("resume", env.Data.ID); err != nil || server.creates != 4 {
		t.Fatal("resume blocked after provider reordering", err, server.creates, out.String())
	}
}

func TestUnknownLaunchRequiresExplicitReconciliation(t *testing.T) {
	server := &launchServer{objects: map[string]map[string]any{}, ambiguous: true}
	a, out, _, cfg := testApp(t, server.serve)
	manifest, plan, dir := launchFiles(t)
	exec := func(args ...string) error {
		out.Reset()
		base := append([]string{"--config", cfg, "meta", "launch"}, args...)
		return executeTest(a, append(base, "--profile", "test", "--account", "123", "--state-dir", dir))
	}
	if err := exec("plan", "--file", manifest, "--save", plan); err != nil {
		t.Fatal(err)
	}
	if err := exec("apply", "--plan", plan); err == nil || output.AsError(err).Exit != 10 {
		t.Fatal(err)
	}
	var env struct {
		Data launch.Run `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	id := env.Data.ID
	if err := exec("resume", id); err == nil || output.AsError(err).Exit != 10 || server.creates != 1 {
		t.Fatal(err, server.creates)
	}
	if err := exec("reconcile", id, "--step", "campaigns.main", "--remote-id", "101"); err != nil {
		t.Fatal(err, out.String())
	}
	if err := exec("resume", id); err != nil || server.creates != 4 {
		t.Fatal(err, server.creates, out.String())
	}
	path, _ := state.Path(dir, id)
	var saved launch.Run
	if err := state.Read(path, &saved); err != nil || !saved.Ready() || len(saved.Decisions) != 1 {
		t.Fatal(err, saved)
	}
	// Local journal reads do not need credentials or profile flags.
	out.Reset()
	if err := executeTest(a, []string{"meta", "launch", "status", id, "--state-dir", dir}); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchResumesVideoSessionAfterUnknownTransfer(t *testing.T) {
	starts, transfers, finishes := 0, 0, 0
	blocked := true
	a, out, _, cfg := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v25.0/act_123":
			_, _ = fmt.Fprint(w, `{"id":"act_123","currency":"USD"}`)
			return
		case "/v25.0/act_123/advideos":
			if r.Method == "GET" {
				_, _ = fmt.Fprint(w, `{"data":[{"id":"789"}]}`)
				return
			}
			if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
				if err := r.ParseMultipartForm(1024); err != nil {
					t.Error(err)
				}
				defer func() {
					if r.MultipartForm != nil {
						_ = r.MultipartForm.RemoveAll()
					}
				}()
				transfers++
				if r.FormValue("upload_session_id") != "123" {
					t.Error("changed session")
				}
				if blocked {
					w.WriteHeader(503)
					return
				}
				_, _ = fmt.Fprint(w, `{"start_offset":"10","end_offset":"10"}`)
				return
			}
			_ = r.ParseForm()
			if r.Form.Get("upload_phase") == "start" {
				starts++
				_, _ = fmt.Fprint(w, `{"upload_session_id":"123","video_id":"789","start_offset":"0","end_offset":"10"}`)
			} else {
				finishes++
				_, _ = fmt.Fprint(w, `{"success":true}`)
			}
			return
		case "/v25.0/789":
			_, _ = fmt.Fprint(w, `{"id":"789","status":{"video_status":"ready"}}`)
			return
		default:
			t.Error("unexpected request", r.URL.Path)
		}
	})
	dir := t.TempDir()
	file := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(file, []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	hash, err := launch.HashFile(file)
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.runtimeForVideoTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Cancel()
	plan := launch.Plan{Steps: []launch.Step{{Key: "assets.demo", Resource: "videos", File: file, Hash: hash}}}
	run, err := launch.NewRun(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.executeRun(r, dir, &run); err == nil || output.AsError(err).Exit != 10 || starts != 1 || transfers != 4 {
		t.Fatal(err, starts, transfers, out.String())
	}
	cp := run.Steps["assets.demo"]
	if cp.Receipt["id"] != "789" {
		t.Fatal("lost video ID", cp)
	}
	blocked = false
	// Read from disk to simulate another process resuming after interruption.
	path, _ := state.Path(dir, run.ID)
	if err = state.Read(path, &run); err != nil {
		t.Fatal(err)
	}
	if err = a.executeRun(r, dir, &run); err != nil {
		t.Fatal(err)
	}
	if starts != 1 || finishes != 1 || transfers != 5 || run.Steps["assets.demo"].ID != "789" || !run.Ready() {
		t.Fatal(starts, finishes, transfers, run)
	}
}

func (a *App) runtimeForVideoTest(cfg string) (*runtime, error) {
	root := a.Root()
	root.SetContext(context.Background())
	cmd, _, err := root.Find([]string{"meta", "videos", "upload"})
	if err != nil {
		return nil, err
	}
	if err = cmd.ParseFlags([]string{"--config", cfg, "--profile", "test", "--account", "123"}); err != nil {
		return nil, err
	}
	cmd.SetContext(context.Background())
	return a.runtime(cmd, true, true)
}

func TestLaunchUnknownVideoStartBlocksOtherCreates(t *testing.T) {
	a, _, _, cfg := testApp(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unknown start allowed a remote operation") })
	r, err := a.runtimeForVideoTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Cancel()
	dir := t.TempDir()
	run, err := launch.NewRun(launch.Plan{Steps: []launch.Step{{Key: "assets.first", Resource: "images"}, {Key: "assets.video", Resource: "videos"}}})
	if err != nil {
		t.Fatal(err)
	}
	run.Steps["assets.video"] = launch.Checkpoint{State: "unknown"}
	if err = state.Save(dir, videoStepID(run.ID, "assets.video"), graph.VideoCheckpoint{Phase: "starting"}); err != nil {
		t.Fatal(err)
	}
	if err = a.executeRun(r, dir, &run); err == nil || output.AsError(err).Exit != 10 {
		t.Fatal(err)
	}
}

func TestLaunchVideoRetryRequiresExplicitUnknownStartDecision(t *testing.T) {
	dir := t.TempDir()
	run, err := launch.NewRun(launch.Plan{Steps: []launch.Step{{Key: "assets.video", Resource: "videos"}}})
	if err != nil {
		t.Fatal(err)
	}
	key := videoStepID(run.ID, "assets.video")
	for _, phase := range []string{"transfer", "starting"} {
		run.Steps["assets.video"] = launch.Checkpoint{State: "unknown"}
		if err = state.Save(dir, key, graph.VideoCheckpoint{Phase: phase}); err != nil {
			t.Fatal(err)
		}
		err = reconcileRun(nil, dir, &run, "assets.video", "", true)
		if phase == "transfer" {
			if err == nil {
				t.Fatal("known session allowed a duplicate-risk restart")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		path, _ := state.Path(dir, key)
		var cp graph.VideoCheckpoint
		if err = state.Read(path, &cp); err != nil {
			t.Fatal(err)
		}
		if cp.Phase != "new" || run.Steps["assets.video"].State != "pending" || len(run.Decisions) != 1 || run.Steps["assets.video"].Receipt["previous_upload"] == nil {
			t.Fatal(cp, run)
		}
	}
}
