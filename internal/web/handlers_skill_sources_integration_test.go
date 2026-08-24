//go:build integration

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/testutil"
)

func TestSkillSourceHandlersUpdateAndRestore(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)
	repoDir := testutil.CreateSkillRepoWithSkills(t,
		testutil.SkillSpec{Name: "alpha-skill", Description: "Alpha skill"},
		testutil.SkillSpec{Name: "beta-skill", Description: "Beta skill"},
	)
	mgr := skills.NewManager(skills.NewStore(gdb), t.TempDir(), nil)
	ctx := context.Background()
	sourceID, err := mgr.RegisterSource(ctx, skills.Source{
		SourceType: "git", URL: repoDir, Ref: "master", SkillFilters: []string{"alpha-skill"},
	})
	if err != nil {
		t.Fatalf("register source: %v", err)
	}
	if err := mgr.RefreshSource(ctx, sourceID); err != nil {
		t.Fatalf("refresh source: %v", err)
	}
	server := &Server{skills: mgr}

	response := callSkillSourceHandler(t, server.handleUpdateSkillSource, http.MethodPut, sourceID, map[string]any{
		"url": repoDir, "ref": "master", "subdir": "", "skill_filters": []string{"beta-skill"},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}
	var updateBody struct {
		Source map[string]any `json:"source"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &updateBody); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	filters, ok := updateBody.Source["skill_filters"].([]any)
	if !ok || len(filters) != 1 || filters[0] != "beta-skill" {
		t.Fatalf("unexpected update response: %#v", updateBody.Source)
	}
	if loaded := mgr.List(); len(loaded) != 1 || loaded[0].Name != "beta-skill" {
		t.Fatalf("unexpected skills after update: %#v", loaded)
	}

	if err := mgr.DeleteSource(ctx, sourceID); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	response = callSkillSourceHandler(t, server.handleRestoreSkillSource, http.MethodPost, sourceID, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", response.Code, response.Body.String())
	}
	restored, err := mgr.GetSource(ctx, sourceID)
	if err != nil || restored.Status != "active" {
		t.Fatalf("unexpected restored source: %#v err=%v", restored, err)
	}
}

func TestUpdateSkillSourceHandlerErrors(t *testing.T) {
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)
	firstRepo := testutil.CreateSkillRepo(t, "alpha-skill", "Alpha skill")
	secondRepo := testutil.CreateSkillRepo(t, "beta-skill", "Beta skill")
	mgr := skills.NewManager(skills.NewStore(gdb), t.TempDir(), nil)
	ctx := context.Background()
	firstID, err := mgr.RegisterSource(ctx, skills.Source{SourceType: "git", URL: firstRepo, Ref: "master"})
	if err != nil {
		t.Fatalf("register first source: %v", err)
	}
	if _, err := mgr.RegisterSource(ctx, skills.Source{SourceType: "git", URL: secondRepo, Ref: "master"}); err != nil {
		t.Fatalf("register second source: %v", err)
	}
	server := &Server{skills: mgr}

	tests := []struct {
		name    string
		id      int64
		payload map[string]any
		status  int
		code    string
	}{
		{name: "invalid input", id: firstID, payload: map[string]any{"url": "", "ref": "master"}, status: http.StatusBadRequest, code: "invalid_skill_source"},
		{name: "not found", id: 999999, payload: map[string]any{"url": firstRepo, "ref": "master"}, status: http.StatusNotFound, code: "not_found"},
		{name: "identity conflict", id: firstID, payload: map[string]any{"url": secondRepo, "ref": "master"}, status: http.StatusConflict, code: "skill_source_conflict"},
		{name: "refresh failure", id: firstID, payload: map[string]any{"url": filepath.Join(t.TempDir(), "missing"), "ref": "master"}, status: http.StatusBadGateway, code: "skill_refresh_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := callSkillSourceHandler(t, server.handleUpdateSkillSource, http.MethodPut, test.id, test.payload)
			if response.Code != test.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.status, response.Body.String())
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != test.code {
				t.Fatalf("unexpected error response: body=%s err=%v", response.Body.String(), err)
			}
		})
	}
}

func callSkillSourceHandler(t *testing.T, handler http.HandlerFunc, method string, sourceID int64, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatalf("encode request: %v", err)
		}
	}
	request := httptest.NewRequest(method, "/api/v1/skill-sources/"+strconv.FormatInt(sourceID, 10), &body)
	request.SetPathValue("sourceID", strconv.FormatInt(sourceID, 10))
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}
