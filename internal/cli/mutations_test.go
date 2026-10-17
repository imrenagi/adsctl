package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"

	"adsctl/internal/output"
)

func TestMutationDefaultsPausedAndVerifies(t *testing.T) {
	posts := 0
	a, out, _, path := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("status") != "PAUSED" || r.Form.Get("special_ad_categories") != "[]" {
				t.Errorf("unsafe defaults: %v", r.Form)
			}
			_, _ = fmt.Fprint(w, `{"id":"456"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"456","account_id":"123","status":"PAUSED","effective_status":"PAUSED","name":"test","objective":"OUTCOME_TRAFFIC","special_ad_categories":[]}`)
	})
	err := executeTest(a, []string{"--config", path, "meta", "campaigns", "create", "--profile", "test", "--account", "123", "--name", "test", "--objective", "OUTCOME_TRAFFIC", "--special-ad-categories", "NONE"})
	if err != nil || posts != 1 || !bytes.Contains(out.Bytes(), []byte(`"state": "verified"`)) {
		t.Fatal(err, posts, out.String())
	}
}
func TestWritesRequireExplicitScopeAndDryRunDoesNotWrite(t *testing.T) {
	t.Setenv("ADSCTL_PROFILE", "test")
	for _, dry := range []bool{false, true} {
		posts := 0
		a, out, _, path := testApp(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				posts++
			}
			_, _ = fmt.Fprint(w, `{"id":"456","account_id":"123"}`)
		})
		args := []string{"--config", path, "meta", "campaigns", "pause", "456", "--account", "123"}
		if dry {
			args = append(args, "--profile", "test", "--dry-run")
		}
		err := executeTest(a, args)
		if posts != 0 {
			t.Fatal("unexpected write")
		}
		if dry {
			if err != nil || !bytes.Contains(out.Bytes(), []byte(`"state": "planned"`)) {
				t.Fatal(err, out.String())
			}
		} else if err == nil || output.AsError(err).Code != "WRITE_SCOPE" {
			t.Fatal(err)
		}
	}
}
func TestAmbiguousCreateIsNotRetried(t *testing.T) {
	posts := 0
	a, out, _, path := testApp(t, func(w http.ResponseWriter, r *http.Request) { posts++; _, _ = fmt.Fprint(w, `unreadable`) })
	err := executeTest(a, []string{"--config", path, "meta", "campaigns", "create", "--profile", "test", "--account", "123", "--name", "test", "--objective", "OUTCOME_TRAFFIC", "--special-ad-categories", "NONE"})
	if err == nil || output.AsError(err).Exit != 10 || posts != 1 || !bytes.Contains(out.Bytes(), []byte(`"state": "unknown"`)) {
		t.Fatal(err, posts, out.String())
	}
}
func TestCrossAccountWriteBlocked(t *testing.T) {
	posts := 0
	a, _, _, path := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			posts++
		}
		_, _ = fmt.Fprint(w, `{"id":"456","account_id":"999"}`)
	})
	err := executeTest(a, []string{"--config", path, "meta", "campaigns", "activate", "456", "--profile", "test", "--account", "123"})
	if err == nil || output.AsError(err).Exit != 4 || posts != 0 {
		t.Fatal(err, posts)
	}
}
func TestVerificationFailurePreservesAppliedReceipt(t *testing.T) {
	a, out, _, path := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			_, _ = fmt.Fprint(w, `{"id":"456"}`)
		} else {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error":{"code":100,"message":"unavailable field"}}`)
		}
	})
	err := executeTest(a, []string{"--config", path, "meta", "campaigns", "create", "--profile", "test", "--account", "123", "--name", "test", "--objective", "OUTCOME_TRAFFIC", "--special-ad-categories", "NONE"})
	if err == nil || output.AsError(err).Exit != 9 || output.AsError(err).HTTPStatus != http.StatusBadRequest || !bytes.Contains(out.Bytes(), []byte(`"id": "456"`)) || !bytes.Contains(out.Bytes(), []byte(`"state": "applied_unverified"`)) || !bytes.Contains(out.Bytes(), []byte(`"verification_error"`)) {
		t.Fatal(err, out.String())
	}
}

func TestStatusMutationsDoNotDuplicateVerificationFields(t *testing.T) {
	for _, args := range [][]string{
		{"meta", "ads", "create", "--profile", "test", "--account", "123", "--name", "ad-1", "--adset", "200", "--creative", "300", "--status", "PAUSED"},
		{"meta", "campaigns", "pause", "456", "--profile", "test", "--account", "123"},
		{"meta", "campaigns", "activate", "456", "--profile", "test", "--account", "123"},
		{"meta", "campaigns", "archive", "456", "--profile", "test", "--account", "123"},
		{"meta", "campaigns", "update", "456", "--profile", "test", "--account", "123", "--status", "PAUSED"},
	} {
		a, out, _, path := testApp(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				_, _ = fmt.Fprint(w, `{"id":"456","success":true}`)
				return
			}
			status := "PAUSED"
			for _, arg := range args {
				switch arg {
				case "activate":
					status = "ACTIVE"
				case "archive":
					status = "ARCHIVED"
				}
			}
			_, _ = fmt.Fprintf(w, `{"id":"456","account_id":"123","adset_id":"200","creative":{"id":"300"},"name":"ad-1","status":%q,"effective_status":%q}`, status, status)
		})
		if err := executeTest(a, append([]string{"--config", path}, args...)); err != nil || !bytes.Contains(out.Bytes(), []byte(`"state": "verified"`)) {
			t.Fatal(args, err, out.String())
		}
	}
}

func TestCreativeMetadataNameUpdateRecoversNormalizedName(t *testing.T) {
	posts := 0
	name := "Launch Video - 2026-10-16"
	a, out, _, path := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			name = r.Form.Get("name")
			_, _ = fmt.Fprint(w, `{"success":true}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"id":"456","account_id":"123","name":%q}`, name)
	})
	// The provider stored a suffixed name at create; an explicit metadata update
	// is the recovery path, without recreating the accepted creative.
	err := executeTest(a, []string{"--config", path, "meta", "creatives", "update", "456", "--profile", "test", "--account", "123", "--name", "Launch Video"})
	if err != nil || posts != 1 || name != "Launch Video" || !bytes.Contains(out.Bytes(), []byte(`"state": "verified"`)) {
		t.Fatal(err, posts, name, out.String())
	}
}

func TestBudgetCurrencyAndObservedAmount(t *testing.T) {
	for _, scenario := range []string{"success", "currency", "campaign-owned", "not-converged"} {
		t.Run(scenario, func(t *testing.T) {
			posts := 0
			amount := "1000"
			a, out, _, path := testApp(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Form.Get("daily_budget") != "9007199254740993" {
						t.Error("budget lost precision", r.Form)
					}
					if scenario != "not-converged" {
						amount = r.Form.Get("daily_budget")
					}
					_, _ = fmt.Fprint(w, `{"success":true}`)
					return
				}
				switch r.URL.Path {
				case "/v25.0/act_123":
					currency := "USD"
					if scenario == "currency" {
						currency = "IDR"
					}
					_, _ = fmt.Fprintf(w, `{"id":"act_123","currency":%q}`, currency)
				case "/v25.0/100":
					budget := "0"
					if scenario == "campaign-owned" {
						budget = "1000"
					}
					_, _ = fmt.Fprintf(w, `{"id":"100","account_id":"123","daily_budget":%q}`, budget)
				default:
					_, _ = fmt.Fprintf(w, `{"id":"456","account_id":"123","campaign_id":"100","daily_budget":%q}`, amount)
				}
			})
			err := executeTest(a, []string{"--config", path, "meta", "adsets", "budget", "set", "456", "--profile", "test", "--account", "123", "--daily-budget-minor", "9007199254740993", "--currency", "USD"})
			switch scenario {
			case "success":
				if err != nil || posts != 1 {
					t.Fatal(err, posts, out.String())
				}
			case "not-converged":
				if err == nil || output.AsError(err).Exit != 9 || posts != 1 {
					t.Fatal(err, posts, out.String())
				}
			default:
				if err == nil || posts != 0 {
					t.Fatal(err, posts)
				}
			}
		})
	}
}
