// Package argo implements workflows.Engine using Argo Workflow CRDs through
// the Kubernetes API. It intentionally depends only on the small workflow
// domain contract from internal/workflows and the standard library.
package argo

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/workflows"
	"github.com/zoncaesaradmin/platformkit/ctxutil"
)

const (
	serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"
	defaultTimeout    = 30 * time.Second
)

type Config struct {
	Namespace   string
	BaseURL     string
	BearerToken string
	HTTPClient  *http.Client
}

type Engine struct {
	namespace string
	baseURL   string
	token     string
	client    *http.Client
}

func New(cfg Config) (*Engine, error) {
	if strings.TrimSpace(cfg.Namespace) == "" {
		return nil, fmt.Errorf("argo: namespace is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("argo: base URL is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &Engine{namespace: cfg.Namespace, baseURL: strings.TrimRight(cfg.BaseURL, "/"), token: cfg.BearerToken, client: client}, nil
}

func NewInCluster(namespace string) (*Engine, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, fmt.Errorf("argo: KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT are required for in-cluster mode")
	}
	token, err := os.ReadFile(filepath.Join(serviceAccountDir, "token"))
	if err != nil {
		return nil, fmt.Errorf("argo: read service account token: %w", err)
	}
	caPool := x509.NewCertPool()
	if ca, err := os.ReadFile(filepath.Join(serviceAccountDir, "ca.crt")); err == nil {
		caPool.AppendCertsFromPEM(ca)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: caPool, MinVersion: tls.VersionTLS12}
	return New(Config{Namespace: namespace, BaseURL: "https://" + host + ":" + port, BearerToken: strings.TrimSpace(string(token)), HTTPClient: &http.Client{Transport: transport, Timeout: defaultTimeout}})
}

func (e *Engine) Submit(ctx context.Context, spec workflows.Spec) error {
	workflow, err := workflowObject(e.namespace, spec)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(workflow)
	_, err = e.do(ctx, http.MethodPost, "/apis/argoproj.io/v1alpha1/namespaces/"+url.PathEscape(e.namespace)+"/workflows", "application/json", body)
	return err
}

func (e *Engine) Status(ctx context.Context, name string) (workflows.Status, error) {
	body, err := e.do(ctx, http.MethodGet, "/apis/argoproj.io/v1alpha1/namespaces/"+url.PathEscape(e.namespace)+"/workflows/"+url.PathEscape(name), "", nil)
	if err != nil {
		return workflows.Status{}, err
	}
	var payload struct {
		Status struct {
			Phase   string `json:"phase"`
			Message string `json:"message"`
		} `json:"status"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return workflows.Status{}, fmt.Errorf("argo: decode workflow status: %w", err)
	}
	return workflows.Status{Phase: mapPhase(payload.Status.Phase), Message: payload.Status.Message}, nil
}

func (e *Engine) Cancel(ctx context.Context, name string) error {
	patch := []byte(`{"spec":{"shutdown":"Terminate"}}`)
	_, err := e.do(ctx, http.MethodPatch, "/apis/argoproj.io/v1alpha1/namespaces/"+url.PathEscape(e.namespace)+"/workflows/"+url.PathEscape(name), "application/merge-patch+json", patch)
	return err
}

func (e *Engine) Logs(ctx context.Context, name string) (string, error) {
	selector := url.QueryEscape("workflows.argoproj.io/workflow=" + name)
	body, err := e.do(ctx, http.MethodGet, "/api/v1/namespaces/"+url.PathEscape(e.namespace)+"/pods?labelSelector="+selector, "", nil)
	if err != nil {
		return "", err
	}
	var pods struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &pods); err != nil {
		return "", fmt.Errorf("argo: decode workflow pods: %w", err)
	}
	if len(pods.Items) == 0 || pods.Items[0].Metadata.Name == "" {
		return "", workflows.ErrNotFound
	}
	logBody, err := e.do(ctx, http.MethodGet, "/api/v1/namespaces/"+url.PathEscape(e.namespace)+"/pods/"+url.PathEscape(pods.Items[0].Metadata.Name)+"/log?container=main", "", nil)
	if err != nil {
		return "", err
	}
	return string(logBody), nil
}

func (e *Engine) do(ctx context.Context, method, path, contentType string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, e.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	traceCtx, traceID := ctxutil.EnsureTraceID(req.Context())
	req = req.WithContext(traceCtx)
	req.Header.Set(ctxutil.TraceIDHeader, traceID)
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("argo: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, workflows.ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("argo: %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

func mapPhase(phase string) workflows.Phase {
	switch strings.ToLower(phase) {
	case "succeeded":
		return workflows.PhaseSucceeded
	case "failed", "error":
		return workflows.PhaseFailed
	case "running":
		return workflows.PhaseRunning
	case "pending", "":
		return workflows.PhasePending
	default:
		return workflows.PhaseRunning
	}
}

func workflowObject(namespace string, spec workflows.Spec) (map[string]any, error) {
	kind := spec.Kind
	if kind == "" {
		kind = workflows.KindBuild
	}
	container, labels, err := workflowContainerSpec(kind, spec)
	if err != nil {
		return nil, err
	}
	workflowSpec := map[string]any{
		"entrypoint":         "main",
		"serviceAccountName": "argo-workflows-argo-workflows-executor",
		"templates": []map[string]any{{
			"name":      "main",
			"container": container,
		}},
	}
	if deadlineSeconds := workflowDeadlineSeconds(spec.Deadline); deadlineSeconds > 0 {
		workflowSpec["activeDeadlineSeconds"] = deadlineSeconds
	}
	if volumes, ok := container["__volumes__"].([]map[string]any); ok && len(volumes) > 0 {
		workflowSpec["volumes"] = volumes
	}
	delete(container, "__volumes__")
	return map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Workflow",
		"metadata": map[string]any{
			"name":      spec.Name,
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": workflowSpec,
	}, nil
}

func workflowContainerSpec(kind workflows.Kind, spec workflows.Spec) (map[string]any, map[string]any, error) {
	if strings.TrimSpace(spec.Name) == "" || strings.TrimSpace(spec.BuilderImageDigest) == "" {
		return nil, nil, fmt.Errorf("argo: workflow spec is missing required fields")
	}
	volumeMounts := []map[string]any{}
	volumes := []map[string]any{}
	env := []map[string]any{}
	if spec.SourceCredentialSecret != "" {
		if spec.KnownHostsSecret == "" {
			return nil, nil, fmt.Errorf("argo: source credential workflows require known_hosts secret")
		}
		volumeMounts = append(volumeMounts, map[string]any{"name": "source-credential", "mountPath": "/var/run/appliance/source-credential", "readOnly": true})
		volumes = append(volumes, map[string]any{"name": "source-credential", "secret": map[string]any{"secretName": spec.SourceCredentialSecret, "defaultMode": 0400}})
		sshCommand := "ssh -i /var/run/appliance/source-credential/ssh-privatekey -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes"
		volumeMounts = append(volumeMounts, map[string]any{"name": "known-hosts", "mountPath": "/var/run/appliance/known-hosts", "readOnly": true})
		volumes = append(volumes, map[string]any{"name": "known-hosts", "secret": map[string]any{"secretName": spec.KnownHostsSecret, "defaultMode": 0444}})
		sshCommand += " -o UserKnownHostsFile=/var/run/appliance/known-hosts/known_hosts"
		env = append(env, map[string]any{"name": "GIT_SSH_COMMAND", "value": sshCommand})
	}

	labels := map[string]any{
		"app.kubernetes.io/part-of":    "appliance",
		"workflows.appliance.local/id": spec.Name,
	}
	var commandScript string
	switch kind {
	case workflows.KindWorkspacePrepare:
		command, err := workspaceCommandScript(spec)
		if err != nil {
			return nil, nil, err
		}
		commandScript = command
		labels["app.kubernetes.io/component"] = "workspace-prepare-workflow"
		labels["workflows.appliance.local/workspace"] = spec.WorkspaceName
		env = append(env,
			map[string]any{"name": "WORKSPACE_ROOT_DIR", "value": spec.WorkspaceRootDir},
			map[string]any{"name": "WORKSPACE_NAME", "value": spec.WorkspaceName},
		)
		volumeMounts = append(volumeMounts, map[string]any{"name": "workspace-storage", "mountPath": spec.WorkspaceRootDir})
		volumes = append(volumes, map[string]any{"name": "workspace-storage", "persistentVolumeClaim": map[string]any{"claimName": spec.WorkspaceClaimName}})
	default:
		command, err := buildCommandScript(spec)
		if err != nil {
			return nil, nil, err
		}
		commandScript = command
		labels["app.kubernetes.io/component"] = "build-workflow"
		labels["workflows.appliance.local/build"] = spec.Name
		containerfile := spec.ContainerfilePath
		if containerfile == "" {
			containerfile = "Containerfile"
		}
		targetImage := spec.TargetRepository + ":" + spec.TargetTag
		env = append(env,
			map[string]any{"name": "SOURCE_REPO_URL", "value": spec.SourceRepoURL},
			map[string]any{"name": "SOURCE_COMMIT_SHA", "value": spec.SourceCommitSHA},
			map[string]any{"name": "CONTAINERFILE_PATH", "value": containerfile},
			map[string]any{"name": "TARGET_IMAGE", "value": targetImage},
		)
		if spec.WorkspaceRootDir != "" && spec.WorkspaceClaimName != "" {
			env = append(env, map[string]any{"name": "WORKSPACE_ROOT_DIR", "value": spec.WorkspaceRootDir})
			volumeMounts = append(volumeMounts, map[string]any{"name": "workspace-storage", "mountPath": spec.WorkspaceRootDir})
			volumes = append(volumes, map[string]any{"name": "workspace-storage", "persistentVolumeClaim": map[string]any{"claimName": spec.WorkspaceClaimName}})
		}
		if spec.ScriptPath != "" {
			env = append(env, map[string]any{"name": "SCRIPT_PATH", "value": spec.ScriptPath})
		}
		if spec.MakeTarget != "" {
			env = append(env, map[string]any{"name": "MAKE_TARGET", "value": spec.MakeTarget})
		}
	}

	container := map[string]any{
		"image":           spec.BuilderImageDigest,
		"imagePullPolicy": "IfNotPresent",
		"command":         []string{"/bin/sh", "-ceu"},
		"args":            []string{commandScript},
		"env":             env,
		"securityContext": map[string]any{
			"allowPrivilegeEscalation": false,
			"capabilities":             map[string]any{"drop": []string{"ALL"}},
		},
		"__volumes__": volumes,
	}
	if len(volumeMounts) > 0 {
		container["volumeMounts"] = volumeMounts
	}
	return container, labels, nil
}

func workflowDeadlineSeconds(deadline time.Time) int64 {
	if deadline.IsZero() {
		return 0
	}
	until := time.Until(deadline)
	if until <= 0 {
		return 1
	}
	seconds := int64(until.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return seconds
}

func buildCommandScript(spec workflows.Spec) (string, error) {
	if spec.SourceRepoURL == "" || spec.SourceCommitSHA == "" || spec.TargetRepository == "" || spec.TargetTag == "" {
		return "", fmt.Errorf("argo: workflow spec is missing required build fields")
	}
	preamble := `mkdir -p /workspace/src
git clone "$SOURCE_REPO_URL" /workspace/src
git -C /workspace/src checkout "$SOURCE_COMMIT_SHA"
cd /workspace/src
`
	switch strings.TrimSpace(spec.Execution) {
	case "", "containerfile":
		return preamble + `buildah bud -f "$CONTAINERFILE_PATH" -t "$TARGET_IMAGE" .
buildah push "$TARGET_IMAGE"`, nil
	case "repo_script":
		if strings.TrimSpace(spec.ScriptPath) == "" {
			return "", fmt.Errorf("argo: repo_script execution requires script path")
		}
		return preamble + `chmod +x "$SCRIPT_PATH"
"./$SCRIPT_PATH"`, nil
	case "make_target":
		if strings.TrimSpace(spec.MakeTarget) == "" {
			return "", fmt.Errorf("argo: make_target execution requires make target")
		}
		return preamble + `make "$MAKE_TARGET" TARGET_IMAGE="$TARGET_IMAGE" CONTAINERFILE_PATH="$CONTAINERFILE_PATH" SOURCE_COMMIT_SHA="$SOURCE_COMMIT_SHA"`, nil
	default:
		return "", fmt.Errorf("argo: unsupported execution mode %q", spec.Execution)
	}
}

func workspaceCommandScript(spec workflows.Spec) (string, error) {
	if strings.TrimSpace(spec.WorkspaceRootDir) == "" || strings.TrimSpace(spec.WorkspaceClaimName) == "" || strings.TrimSpace(spec.WorkspaceName) == "" || len(spec.WorkspaceRepos) == 0 {
		return "", fmt.Errorf("argo: workspace workflow spec is missing required fields")
	}
	var b strings.Builder
	b.WriteString("workspace_dir=\"$WORKSPACE_ROOT_DIR/$WORKSPACE_NAME\"\n")
	b.WriteString("mkdir -p \"$workspace_dir\"\n")
	b.WriteString("cd \"$workspace_dir\"\n")
	for _, repo := range spec.WorkspaceRepos {
		if strings.TrimSpace(repo.Name) == "" || strings.TrimSpace(repo.URL) == "" || strings.TrimSpace(repo.Ref) == "" {
			return "", fmt.Errorf("argo: workspace repo spec is missing required fields")
		}
		repoDir := shellQuote(repo.Name)
		repoURL := shellQuote(repo.URL)
		repoRef := shellQuote(repo.Ref)
		b.WriteString("if [ -d ")
		b.WriteString(repoDir)
		b.WriteString("/.git ]; then\n")
		b.WriteString("  echo \"repo ")
		b.WriteString(repo.Name)
		b.WriteString(" already present; skipping\"\n")
		b.WriteString("else\n")
		b.WriteString("  if [ -e ")
		b.WriteString(repoDir)
		b.WriteString(" ]; then rm -rf ")
		b.WriteString(repoDir)
		b.WriteString("; fi\n")
		b.WriteString("  git clone ")
		b.WriteString(repoURL)
		b.WriteString(" ")
		b.WriteString(repoDir)
		b.WriteString("\n")
		b.WriteString("  git -C ")
		b.WriteString(repoDir)
		b.WriteString(" checkout ")
		b.WriteString(repoRef)
		b.WriteString("\n")
		b.WriteString("fi\n")
	}
	return b.String(), nil
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'"
}

var _ workflows.Engine = (*Engine)(nil)
