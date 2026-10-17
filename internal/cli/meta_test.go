package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"adsctl/internal/config"
)

func testApp(t *testing.T, h http.HandlerFunc) (*App, *bytes.Buffer, *bytes.Buffer, string) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fields := r.URL.Query().Get("fields"); fields != "" && !strings.Contains(fields, "{") {
			seen := map[string]bool{}
			for _, f := range strings.Split(fields, ",") {
				f = strings.TrimSpace(f)
				if f == "" {
					continue
				}
				if seen[f] {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = fmt.Fprintf(w, `{"error":{"code":2500,"type":"OAuthException","message":"Syntax error \"Field %s specified more than once. This is only possible before version 2.1\""}}`, f)
					return
				}
				seen[f] = true
			}
		}
		h(w, r)
	}))
	t.Cleanup(s.Close)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{"test": {Platform: "meta", TokenEnv: "ADSCTL_FIXTURE_TOKEN", APIVersion: "v25.0", Mode: "write"}}}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADSCTL_FIXTURE_TOKEN", "fixture-token")
	out, errOut := new(bytes.Buffer), new(bytes.Buffer)
	a := &App{In: new(bytes.Buffer), Out: out, Err: errOut, baseURL: s.URL}
	return a, out, errOut, path
}
func executeTest(a *App, args []string) error {
	root := a.Root()
	root.SetOut(a.Out)
	root.SetErr(a.Err)
	root.SetIn(a.In)
	root.SetArgs(args)
	return root.ExecuteContext(context.Background())
}
func TestMetaReadAndCrossAccountGuard(t *testing.T) {
	a, out, _, path := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v25.0/456" {
			_, _ = fmt.Fprint(w, `{"id":"456","account_id":"999"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"456","name":"example"}]}`)
	})
	if err := executeTest(a, []string{"--config", path, "meta", "campaigns", "list", "--profile", "test", "--account", "act_123"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"ok": true`)) {
		t.Fatal(out.String())
	}
	a2, _, _, path2 := testApp(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{"id":"456","account_id":"999"}`) })
	if err := executeTest(a2, []string{"--config", path2, "meta", "ads", "get", "456", "--profile", "test", "--account", "act_123"}); err == nil {
		t.Fatal("cross-account read allowed")
	}
}
func TestDiscoveryDoesNotRequireCredentials(t *testing.T) {
	for _, args := range [][]string{{"commands", "--platform", "meta"}, {"meta", "schema", "get", "campaigns", "--operation", "list"}, {"meta", "--help"}} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, new(bytes.Buffer), &out, &errOut); code != 0 {
			t.Fatal(args, code, errOut.String())
		}
	}
}

func TestHomeConfigurationAndRelativeSecretAuthenticate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ADSCTL_HOME", home)
	if err := os.Mkdir(filepath.Join(home, "secrets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "secrets", "meta-token"), []byte("fixture-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{In: new(bytes.Buffer), Out: new(bytes.Buffer), Err: new(bytes.Buffer)}
	if err := executeTest(a, []string{"config", "profiles", "set", "read", "--token-file", "secrets/meta-token"}); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("relative token was not used")
		}
		_, _ = fmt.Fprint(w, `{"id":"act_123","currency":"USD"}`)
	}))
	defer s.Close()
	a = &App{In: new(bytes.Buffer), Out: new(bytes.Buffer), Err: new(bytes.Buffer), baseURL: s.URL}
	if err := executeTest(a, []string{"meta", "auth", "check", "--profile", "read", "--account", "act_123"}); err != nil {
		t.Fatal(err)
	}
}
