package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"appliance-code/sdk/golang/applianceclient"
)

type config struct {
	apiBaseURL    string
	serverBinary  string
	dataDir       string
	configFile    string
	publicAddr    string
	internalAddr  string
	adminUsername string
	adminPassword string
	alicePassword string
	resetPassword string
	logFile       string
}

type runner struct {
	cfg    config
	client *applianceclient.Client
	http   *http.Client
	logger *log.Logger
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.apiBaseURL, "api-base-url", "", "control-plane API base URL (required)")
	flag.StringVar(&cfg.serverBinary, "server-binary", "", "path to appliance-server binary (required)")
	flag.StringVar(&cfg.dataDir, "data-dir", "", "control-plane data directory (required)")
	flag.StringVar(&cfg.configFile, "config-file", "", "control-plane config file used by node-local commands")
	flag.StringVar(&cfg.publicAddr, "public-addr", "", "public listen addr used by the local server")
	flag.StringVar(&cfg.internalAddr, "internal-addr", "", "internal listen addr used by the local server")
	flag.StringVar(&cfg.adminUsername, "admin-username", "admin", "bootstrap administrator username")
	flag.StringVar(&cfg.adminPassword, "admin-password", "", "bootstrap administrator password (required)")
	flag.StringVar(&cfg.alicePassword, "alice-password", "", "initial alice password (required)")
	flag.StringVar(&cfg.resetPassword, "reset-password", "", "recovery-reset replacement password (required)")
	flag.StringVar(&cfg.logFile, "log-file", "", "path to the client log file")
	flag.Parse()

	switch {
	case cfg.apiBaseURL == "":
		fatalf("missing --api-base-url")
	case cfg.serverBinary == "":
		fatalf("missing --server-binary")
	case cfg.dataDir == "":
		fatalf("missing --data-dir")
	case cfg.adminPassword == "":
		fatalf("missing --admin-password")
	case cfg.alicePassword == "":
		fatalf("missing --alice-password")
	case cfg.resetPassword == "":
		fatalf("missing --reset-password")
	}

	logger := log.New(os.Stdout, "e2e: ", log.LstdFlags|log.Lmicroseconds)
	if cfg.logFile != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.logFile), 0o755); err != nil {
			fatalf("creating log dir: %v", err)
		}
		f, err := os.Create(cfg.logFile)
		if err != nil {
			fatalf("creating log file: %v", err)
		}
		defer f.Close()
		logger = log.New(io.MultiWriter(os.Stdout, f), "e2e: ", log.LstdFlags|log.Lmicroseconds)
	}

	r := &runner{
		cfg:    cfg,
		client: applianceclient.New(cfg.apiBaseURL),
		http:   &http.Client{Timeout: 15 * time.Second},
		logger: logger,
	}

	if err := r.run(context.Background()); err != nil {
		logger.Printf("FAILED: %v", err)
		os.Exit(1)
	}
	logger.Print("PASSED")
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}

func (r *runner) run(ctx context.Context) error {
	r.logger.Print("logging in as admin")
	adminLogin, err := r.client.Login(ctx, r.cfg.adminUsername, r.cfg.adminPassword)
	if err != nil {
		return fmt.Errorf("admin login: %w", err)
	}
	adminAccess := adminLogin.AccessToken

	adminSession, err := r.client.Session(ctx, adminAccess)
	if err != nil {
		return fmt.Errorf("admin session: %w", err)
	}
	if adminSession.AuthMethod != "session" {
		return fmt.Errorf("admin session auth method = %q, want session", adminSession.AuthMethod)
	}

	refreshed, err := r.client.Refresh(ctx, adminLogin.RefreshToken)
	if err != nil {
		return fmt.Errorf("admin refresh: %w", err)
	}
	if refreshed.AccessToken == adminAccess {
		return fmt.Errorf("admin refresh returned the same access token")
	}
	adminAccess = refreshed.AccessToken

	adminToken, err := r.client.CreateToken(ctx, adminAccess, applianceclient.CreateTokenRequest{Name: "e2e-admin-token"})
	if err != nil {
		return fmt.Errorf("admin create token: %w", err)
	}
	adminTokens, err := r.client.ListTokens(ctx, adminAccess)
	if err != nil {
		return fmt.Errorf("admin list tokens: %w", err)
	}
	if !containsToken(adminTokens, adminToken.ID) {
		return fmt.Errorf("admin token %s not present in list", adminToken.ID)
	}

	r.logger.Print("creating and managing alice user")
	alice, err := r.client.CreateUser(ctx, adminAccess, applianceclient.CreateUserRequest{
		Username:    "alice",
		DisplayName: "Alice",
		Password:    r.cfg.alicePassword,
	})
	if err != nil {
		return fmt.Errorf("create alice: %w", err)
	}
	if _, err := r.client.UpdateUserDisplayName(ctx, adminAccess, alice.ID, "Alice Example"); err != nil {
		return fmt.Errorf("update alice display name: %w", err)
	}
	users, err := r.client.ListUsers(ctx, adminAccess)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	if !containsUser(users, alice.ID) {
		return fmt.Errorf("alice not present in user list")
	}
	gotAlice, err := r.client.GetUser(ctx, adminAccess, alice.ID)
	if err != nil {
		return fmt.Errorf("get alice: %w", err)
	}
	if gotAlice.DisplayName != "Alice Example" {
		return fmt.Errorf("alice display name = %q, want Alice Example", gotAlice.DisplayName)
	}
	if err := r.client.UnlockUser(ctx, adminAccess, alice.ID); err != nil {
		return fmt.Errorf("unlock alice: %w", err)
	}

	r.logger.Print("exercising role and permission APIs")
	permissions, err := r.client.ListPermissions(ctx, adminAccess)
	if err != nil {
		return fmt.Errorf("list permissions: %w", err)
	}
	if !containsPermission(permissions, "users.read") {
		return fmt.Errorf("permission catalog missing users.read")
	}
	roles, err := r.client.ListRoles(ctx, adminAccess)
	if err != nil {
		return fmt.Errorf("list roles: %w", err)
	}
	if !containsRoleNamed(roles, "administrator") {
		return fmt.Errorf("built-in administrator role missing")
	}

	disposableRole, err := r.client.CreateRole(ctx, adminAccess, applianceclient.CreateRoleRequest{
		Name:        "e2e-delete-me",
		Permissions: []string{"tokens.read.self"},
	})
	if err != nil {
		return fmt.Errorf("create disposable role: %w", err)
	}
	if err := r.client.DeleteRole(ctx, adminAccess, disposableRole.ID); err != nil {
		return fmt.Errorf("delete disposable role: %w", err)
	}

	aliceRole, err := r.client.CreateRole(ctx, adminAccess, applianceclient.CreateRoleRequest{
		Name:        "e2e-alice-role",
		Permissions: []string{"tokens.read.self"},
	})
	if err != nil {
		return fmt.Errorf("create alice role: %w", err)
	}
	if err := r.client.UpdateRole(ctx, adminAccess, aliceRole.ID, []string{
		"tokens.read.self",
		"tokens.create.self",
		"tokens.revoke.self",
		"registry.pull",
		"work_profiles.read",
		"workspaces.create",
		"workspaces.read.self",
		"workspaces.delete.self",
		"build_targets.read",
		"builds.create",
		"builds.read.self",
		"builds.cancel.self",
		"jobs.read.self",
		"jobs.cancel.self",
		"mcp.invoke",
	}); err != nil {
		return fmt.Errorf("update alice role: %w", err)
	}
	if err := r.client.SetUserRoles(ctx, adminAccess, alice.ID, []string{aliceRole.ID}); err != nil {
		return fmt.Errorf("set alice roles: %w", err)
	}

	r.logger.Print("verifying disable/enable and alice login")
	if err := r.client.DisableUser(ctx, adminAccess, alice.ID); err != nil {
		return fmt.Errorf("disable alice: %w", err)
	}
	if _, err := r.client.Login(ctx, "alice", r.cfg.alicePassword); err == nil {
		return fmt.Errorf("alice login succeeded while disabled")
	} else if problemCode(err) != "invalid_credentials" {
		return fmt.Errorf("alice disabled login error = %v, want invalid_credentials", err)
	}
	if err := r.client.EnableUser(ctx, adminAccess, alice.ID); err != nil {
		return fmt.Errorf("enable alice: %w", err)
	}

	aliceLogin, err := r.client.Login(ctx, "alice", r.cfg.alicePassword)
	if err != nil {
		return fmt.Errorf("alice login after enable: %w", err)
	}
	aliceAccess := aliceLogin.AccessToken

	if _, err := r.client.ListUsers(ctx, aliceAccess); err == nil {
		return fmt.Errorf("alice unexpectedly listed users")
	} else if problemCode(err) != "forbidden" {
		return fmt.Errorf("alice list users error = %v, want forbidden", err)
	}

	aliceToken, err := r.client.CreateToken(ctx, aliceAccess, applianceclient.CreateTokenRequest{Name: "alice-self-token"})
	if err != nil {
		return fmt.Errorf("alice create self token: %w", err)
	}
	aliceTokens, err := r.client.ListTokens(ctx, aliceAccess)
	if err != nil {
		return fmt.Errorf("alice list self tokens: %w", err)
	}
	if !containsToken(aliceTokens, aliceToken.ID) {
		return fmt.Errorf("alice self token missing from list")
	}

	r.logger.Print("exercising password reset and node-local recovery reset")
	resetStart, err := r.client.InitiatePasswordReset(ctx, adminAccess, alice.ID)
	if err != nil {
		return fmt.Errorf("initiate password reset: %w", err)
	}
	if resetStart.ResetCredential == "" {
		return fmt.Errorf("password reset credential was empty")
	}

	if err := r.runRecoveryResetPassword(ctx, "alice", r.cfg.resetPassword); err != nil {
		return fmt.Errorf("recovery reset-password: %w", err)
	}
	if _, err := r.client.Login(ctx, "alice", r.cfg.alicePassword); err == nil {
		return fmt.Errorf("alice old password still works after recovery reset")
	}
	aliceLogin, err = r.client.Login(ctx, "alice", r.cfg.resetPassword)
	if err != nil {
		return fmt.Errorf("alice login after recovery reset: %w", err)
	}
	aliceAccess = aliceLogin.AccessToken

	r.logger.Print("exercising admin create-for-user token and registry flows")
	aliceRegistryToken, err := r.client.CreateTokenForUser(ctx, adminAccess, alice.ID, applianceclient.CreateTokenRequest{
		Name:   "alice-registry-token",
		Scopes: []string{"registry.pull"},
	})
	if err != nil {
		return fmt.Errorf("admin create token for alice: %w", err)
	}
	grant, err := r.client.CreateRegistryGrant(ctx, adminAccess, applianceclient.CreateRegistryGrantRequest{
		SubjectType: "user",
		SubjectID:   alice.ID,
		PathPrefix:  "ci/pipeline-a",
		Actions:     []string{"pull"},
	})
	if err != nil {
		return fmt.Errorf("create registry grant: %w", err)
	}
	grants, err := r.client.ListRegistryGrants(ctx, adminAccess)
	if err != nil {
		return fmt.Errorf("list registry grants: %w", err)
	}
	if !containsGrant(grants, grant.ID) {
		return fmt.Errorf("registry grant %s not present in list", grant.ID)
	}
	registryToken, err := r.client.RegistryToken(ctx, "alice", aliceRegistryToken.Token, "zot", []string{"repository:ci/pipeline-a/app:pull"})
	if err != nil {
		return fmt.Errorf("registry token issuance: %w", err)
	}
	if registryToken.Token == "" {
		return fmt.Errorf("registry token response was empty")
	}
	repositories, err := r.client.ListRegistryRepositories(ctx, aliceAccess)
	if err != nil {
		return fmt.Errorf("list registry repositories: %w", err)
	}
	if repositories == nil {
		return fmt.Errorf("list registry repositories returned nil slice")
	}
	if _, err := r.client.ConfigureBuilderGitAccess(ctx, adminAccess, applianceclient.ConfigureBuilderGitAccessRequest{
		Host:     "git.internal.example.com",
		Username: "builder-user",
		Token:    "builder-token",
	}); err != nil {
		return fmt.Errorf("configure builder Git access: %w", err)
	}

	r.logger.Print("exercising build APIs")
	build, err := r.client.CreateBuild(ctx, aliceAccess, applianceclient.CreateBuildRequest{
		SourceRepoURL:      "https://git.internal.example.com/team/app",
		SourceCommitSHA:    "0123456789abcdef0123456789abcdef01234567",
		ImageRepository:    "users/alice/app",
		ImageTag:           "v1",
		BuilderImageDigest: "buildah@sha256:approved",
	})
	if err != nil {
		return fmt.Errorf("create build: %w", err)
	}
	buildList, err := r.client.ListBuilds(ctx, aliceAccess)
	if err != nil {
		return fmt.Errorf("list builds: %w", err)
	}
	if !containsBuild(buildList, build.ID) {
		return fmt.Errorf("build %s not present in list", build.ID)
	}
	gotBuild, err := r.client.GetBuild(ctx, aliceAccess, build.ID)
	if err != nil {
		return fmt.Errorf("get build: %w", err)
	}
	if gotBuild.OwnerID != alice.ID {
		return fmt.Errorf("build owner = %q, want %q", gotBuild.OwnerID, alice.ID)
	}
	buildLogs, err := r.client.BuildLogs(ctx, aliceAccess, build.ID)
	if err != nil {
		return fmt.Errorf("build logs: %w", err)
	}
	if !strings.Contains(buildLogs, "fake logs for workflow") {
		return fmt.Errorf("unexpected build logs: %q", buildLogs)
	}
	cancelled, err := r.client.CancelBuild(ctx, aliceAccess, build.ID)
	if err != nil {
		return fmt.Errorf("cancel build: %w", err)
	}
	if cancelled.ID != build.ID {
		return fmt.Errorf("cancel build returned id %q, want %q", cancelled.ID, build.ID)
	}

	r.logger.Print("exercising developer workflow REST APIs through SDK")
	profiles, err := r.client.ListWorkProfiles(ctx, aliceAccess)
	if err != nil {
		return fmt.Errorf("list workspace profiles: %w", err)
	}
	if !containsWorkProfile(profiles, "builder") {
		return fmt.Errorf("workspace profile builder not present: %#v", profiles)
	}
	workspace, err := r.client.CreateWorkspace(ctx, aliceAccess, applianceclient.CreateWorkspaceRequest{
		Name:        "sdk-app",
		WorkProfile: "builder",
	})
	if err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	workspaces, err := r.client.ListWorkspaces(ctx, aliceAccess)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}
	if !containsWorkspace(workspaces, workspace.ID) {
		return fmt.Errorf("workspace %s not present in list", workspace.ID)
	}
	gotWorkspace, err := r.client.GetWorkspace(ctx, aliceAccess, workspace.ID)
	if err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}
	if gotWorkspace.ID != workspace.ID {
		return fmt.Errorf("get workspace id = %q, want %q", gotWorkspace.ID, workspace.ID)
	}
	currentWorkspace, err := r.client.CurrentWorkspace(ctx, aliceAccess)
	if err != nil {
		return fmt.Errorf("current workspace: %w", err)
	}
	if currentWorkspace.ID != workspace.ID {
		return fmt.Errorf("current workspace id = %q, want %q", currentWorkspace.ID, workspace.ID)
	}
	if _, err := r.client.SetCurrentWorkspace(ctx, aliceAccess, workspace.ID); err != nil {
		return fmt.Errorf("set current workspace: %w", err)
	}
	targets, err := r.client.ListCurrentBuildTargets(ctx, aliceAccess)
	if err != nil {
		return fmt.Errorf("list current build targets: %w", err)
	}
	if !containsBuildTarget(targets, "app") {
		return fmt.Errorf("build target app not present: %#v", targets)
	}
	job, err := r.client.SubmitCurrentBuildWithIdempotencyKey(ctx, aliceAccess, applianceclient.SubmitCurrentBuildRequest{
		TargetName: "app",
		ImageTag:   "sdk-v1",
	}, "sdk-workflow-build")
	if err != nil {
		return fmt.Errorf("submit current build: %w", err)
	}
	if job.ArtifactRef != "users/alice/app:sdk-v1" {
		return fmt.Errorf("submitted job artifactRef = %q, want users/alice/app:sdk-v1", job.ArtifactRef)
	}
	currentJob, err := r.client.CurrentWorkspaceBuildStatus(ctx, aliceAccess)
	if err != nil {
		return fmt.Errorf("current workspace build status: %w", err)
	}
	if currentJob.ID != job.ID {
		return fmt.Errorf("current workspace build status id = %q, want %q", currentJob.ID, job.ID)
	}
	jobs, err := r.client.ListJobs(ctx, aliceAccess)
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}
	if !containsJob(jobs, job.ID) {
		return fmt.Errorf("job %s not present in list", job.ID)
	}
	gotJob, err := r.client.GetJob(ctx, aliceAccess, job.ID)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}
	if gotJob.ArtifactRef != job.ArtifactRef {
		return fmt.Errorf("get job artifactRef = %q, want %q", gotJob.ArtifactRef, job.ArtifactRef)
	}
	steps, err := r.client.JobSteps(ctx, aliceAccess, job.ID)
	if err != nil {
		return fmt.Errorf("job steps: %w", err)
	}
	if len(steps) == 0 {
		return fmt.Errorf("job steps were empty")
	}
	jobLogs, err := r.client.JobLogs(ctx, aliceAccess, job.ID)
	if err != nil {
		return fmt.Errorf("job logs: %w", err)
	}
	if !strings.Contains(jobLogs, "fake logs for workflow") {
		return fmt.Errorf("unexpected job logs: %q", jobLogs)
	}
	cancelledJob, err := r.client.CancelJob(ctx, aliceAccess, job.ID)
	if err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}
	if cancelledJob.ID != job.ID {
		return fmt.Errorf("cancel job returned id %q, want %q", cancelledJob.ID, job.ID)
	}
	if err := r.client.DeleteWorkspace(ctx, aliceAccess, workspace.ID); err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}

	r.logger.Print("exercising live MCP endpoint")
	if err := r.exerciseMCP(ctx, aliceAccess); err != nil {
		return fmt.Errorf("exercise /mcp: %w", err)
	}

	r.logger.Print("cleaning up tokens, grants, roles, and sessions")
	if err := r.client.DeleteRegistryGrant(ctx, adminAccess, grant.ID); err != nil {
		return fmt.Errorf("delete registry grant: %w", err)
	}
	if err := r.client.RevokeTokenForUser(ctx, adminAccess, alice.ID, aliceRegistryToken.ID); err != nil {
		return fmt.Errorf("revoke admin-created alice token: %w", err)
	}
	if err := r.client.RevokeToken(ctx, aliceAccess, aliceToken.ID); err != nil {
		return fmt.Errorf("revoke alice self token: %w", err)
	}
	if err := r.client.SetUserRoles(ctx, adminAccess, alice.ID, nil); err != nil {
		return fmt.Errorf("clear alice roles: %w", err)
	}
	if err := r.client.DeleteRole(ctx, adminAccess, aliceRole.ID); err != nil {
		return fmt.Errorf("delete alice role: %w", err)
	}
	if err := r.client.RevokeToken(ctx, adminAccess, adminToken.ID); err != nil {
		return fmt.Errorf("revoke admin token: %w", err)
	}
	if err := r.client.Logout(ctx, aliceAccess); err != nil {
		return fmt.Errorf("alice logout: %w", err)
	}
	if err := r.client.Logout(ctx, adminAccess); err != nil {
		return fmt.Errorf("admin logout: %w", err)
	}
	return nil
}

func (r *runner) runRecoveryResetPassword(ctx context.Context, username, newPassword string) error {
	passwordFile := filepath.Join(os.TempDir(), "appliance-e2e-reset-password.txt")
	if err := os.WriteFile(passwordFile, []byte(newPassword+"\n"), 0o600); err != nil {
		return err
	}
	defer os.Remove(passwordFile)

	cmd := exec.CommandContext(ctx, r.cfg.serverBinary, "recovery", "reset-password", "--username", username, "--password-file", passwordFile)
	cmd.Env = append(os.Environ(),
		"APPLIANCE_CONFIG_FILE="+r.cfg.configFile,
		"APPLIANCE_PROFILE=builder",
		"APPLIANCE_DATA_DIR="+r.cfg.dataDir,
		"APPLIANCE_CANONICAL_ORIGIN="+r.cfg.apiBaseURL,
		"APPLIANCE_PUBLIC_ADDR="+r.cfg.publicAddr,
		"APPLIANCE_INTERNAL_ADDR="+r.cfg.internalAddr,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	r.logger.Printf("recovery reset-password output: %s", strings.TrimSpace(string(out)))
	return nil
}

func (r *runner) exerciseMCP(ctx context.Context, bearer string) error {
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      "init-1",
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"clientInfo": map[string]any{
				"name":    "appliance-e2e",
				"version": "1.0.0",
			},
		},
	}
	initResp, sessionID, err := r.mcpPOST(ctx, bearer, "", initReq)
	if err != nil {
		return err
	}
	result, ok := initResp["result"].(map[string]any)
	if !ok || result["protocolVersion"] == "" {
		return fmt.Errorf("initialize response missing protocolVersion: %#v", initResp)
	}
	if sessionID == "" {
		return fmt.Errorf("initialize response missing Mcp-Session-Id header")
	}

	pingResp, _, err := r.mcpPOST(ctx, bearer, sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      "ping-1",
		"method":  "ping",
	})
	if err != nil {
		return err
	}
	if _, ok := pingResp["result"]; !ok {
		return fmt.Errorf("ping response missing result: %#v", pingResp)
	}

	toolsResp, _, err := r.mcpPOST(ctx, bearer, sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      "tools-1",
		"method":  "tools/list",
	})
	if err != nil {
		return err
	}
	toolsResult, ok := toolsResp["result"].(map[string]any)
	if !ok {
		return fmt.Errorf("tools/list response missing result: %#v", toolsResp)
	}
	if _, ok := toolsResult["tools"]; !ok {
		return fmt.Errorf("tools/list response missing tools: %#v", toolsResp)
	}
	toolNames, err := mcpToolNames(toolsResult["tools"])
	if err != nil {
		return fmt.Errorf("decode tools/list tools: %w", err)
	}
	for _, expected := range []string{
		"list_work_profiles",
		"get_workspace",
		"set_workspace",
		"list_build_targets",
		"submit_build",
		"list_jobs",
		"get_job_status",
		"get_job_steps",
		"get_job_logs",
		"cancel_job",
	} {
		if !toolNames[expected] {
			return fmt.Errorf("tools/list missing builder MCP tool %q: %#v", expected, toolsResult["tools"])
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, r.cfg.apiBaseURL+"/mcp", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Mcp-Session-Id", sessionID)
	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE /mcp status = %d body=%s", resp.StatusCode, string(body))
	}
	return nil
}

func (r *runner) mcpPOST(ctx context.Context, bearer, sessionID string, payload map[string]any) (map[string]any, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.apiBaseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("POST /mcp status = %d body=%s", resp.StatusCode, string(responseBody))
	}
	var decoded map[string]any
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, "", err
	}
	return decoded, resp.Header.Get("Mcp-Session-Id"), nil
}

func containsUser(users []applianceclient.User, userID string) bool {
	for _, u := range users {
		if u.ID == userID {
			return true
		}
	}
	return false
}

func containsRoleNamed(roles []applianceclient.Role, name string) bool {
	for _, role := range roles {
		if role.Name == name {
			return true
		}
	}
	return false
}

func containsPermission(perms []applianceclient.Permission, name string) bool {
	for _, p := range perms {
		if p.Name == name {
			return true
		}
	}
	return false
}

func containsToken(tokens []applianceclient.APIToken, tokenID string) bool {
	for _, tok := range tokens {
		if tok.ID == tokenID {
			return true
		}
	}
	return false
}

func containsGrant(grants []applianceclient.RegistryGrant, grantID string) bool {
	for _, grant := range grants {
		if grant.ID == grantID {
			return true
		}
	}
	return false
}

func containsBuild(builds []applianceclient.Build, buildID string) bool {
	for _, build := range builds {
		if build.ID == buildID {
			return true
		}
	}
	return false
}

func containsWorkProfile(profiles []applianceclient.WorkProfile, name string) bool {
	for _, profile := range profiles {
		if profile.Name == name {
			return true
		}
	}
	return false
}

func containsWorkspace(workspaces []applianceclient.Workspace, workspaceID string) bool {
	for _, workspace := range workspaces {
		if workspace.ID == workspaceID {
			return true
		}
	}
	return false
}

func containsBuildTarget(targets []applianceclient.BuildTarget, nameOrAlias string) bool {
	for _, target := range targets {
		if target.Name == nameOrAlias {
			return true
		}
		for _, alias := range target.Aliases {
			if alias == nameOrAlias {
				return true
			}
		}
	}
	return false
}

func containsJob(jobs []applianceclient.Job, jobID string) bool {
	for _, job := range jobs {
		if job.ID == jobID {
			return true
		}
	}
	return false
}

func mcpToolNames(raw any) (map[string]bool, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("tools has type %T, want array", raw)
	}
	names := make(map[string]bool, len(items))
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool has type %T, want object", item)
		}
		name, ok := tool["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("tool missing non-empty name: %#v", tool)
		}
		names[name] = true
	}
	return names, nil
}

func problemCode(err error) string {
	var problem *applianceclient.Problem
	if ok := errorAs(err, &problem); ok {
		return problem.Code
	}
	return ""
}

func errorAs(err error, target **applianceclient.Problem) bool {
	if err == nil {
		return false
	}
	problem, ok := err.(*applianceclient.Problem)
	if !ok {
		return false
	}
	*target = problem
	return true
}
