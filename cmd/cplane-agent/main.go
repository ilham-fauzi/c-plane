package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const version = "0.1.0"

type Config struct {
	HostID       string
	APIURL       string
	MQTTURL      string
	StateDir     string
	LogDir       string
	TokenFile    string
	PollInterval time.Duration
}

type Client struct {
	config Config
	http   *http.Client
}

type DeploymentJob struct {
	ID           string `json:"id"`
	AppID        string `json:"app_id"`
	HostID       string `json:"host_id"`
	Action       string `json:"action"`
	Status       string `json:"status"`
	Ref          string `json:"ref,omitempty"`
	ReleaseID    string `json:"release_id,omitempty"`
	MetadataJSON string `json:"metadata_json,omitempty"`
}

type SetupAppMetadata struct {
	AppName      string `json:"app_name"`
	RootPath     string `json:"root_path"`
	Domain       string `json:"domain,omitempty"`
	Runtime      string `json:"runtime"`
	RecipePath   string `json:"recipe_path"`
	NginxEnabled bool   `json:"nginx_enabled"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "register":
		runRegister(os.Args[2:])
	case "run":
		runAgent(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func runRegister(args []string) {
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	configPath := fs.String("config", "/etc/c-plane/agent.toml", "agent config path")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	config, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	client := NewClient(config)
	token, err := readToken(config.TokenFile)
	if err != nil {
		log.Fatalf("read install token: %v", err)
	}

	hostname, _ := os.Hostname()
	var response struct {
		AgentToken   string `json:"agent_token"`
		MQTTURL      string `json:"mqtt_url"`
		MQTTUsername string `json:"mqtt_username"`
		MQTTPassword string `json:"mqtt_password"`
	}
	err = client.doJSON(context.Background(), http.MethodPost, "/api/agent/register", map[string]string{
		"host_id":       config.HostID,
		"install_token": token,
		"agent_version": version,
		"hostname":      hostname,
		"os":            runtime.GOOS,
		"arch":          runtime.GOARCH,
	}, &response)
	if err != nil {
		log.Fatalf("register agent: %v", err)
	}
	if response.AgentToken == "" {
		log.Fatal("register agent: empty agent token")
	}
	if err := writeSecret(config.TokenFile, response.AgentToken); err != nil {
		log.Fatalf("write agent token: %v", err)
	}
	log.Printf("agent registered host_id=%s", config.HostID)
}

func runAgent(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", "/etc/c-plane/agent.toml", "agent config path")
	once := fs.Bool("once", false, "run one polling cycle and exit")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	config, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if config.PollInterval == 0 {
		config.PollInterval = 15 * time.Second
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := NewClient(config)
	if *once {
		if err := client.tick(ctx); err != nil {
			log.Fatal(err)
		}
		return
	}

	ticker := time.NewTicker(config.PollInterval)
	defer ticker.Stop()

	for {
		if err := client.tick(ctx); err != nil {
			log.Printf("agent tick failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func NewClient(config Config) *Client {
	return &Client{
		config: config,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) tick(ctx context.Context) error {
	if err := c.heartbeat(ctx); err != nil {
		return err
	}
	jobs, err := c.pendingJobs(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := c.handleJob(ctx, job); err != nil {
			log.Printf("job %s failed: %v", job.ID, err)
		}
	}
	return nil
}

func (c *Client) heartbeat(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodPost, "/api/agent/heartbeat", map[string]any{
		"host_id":       c.config.HostID,
		"agent_version": version,
		"status":        "online",
		"running_jobs":  0,
	}, nil)
}

func (c *Client) pendingJobs(ctx context.Context) ([]DeploymentJob, error) {
	var jobs []DeploymentJob
	path := "/api/agent/jobs/pending?host_id=" + c.config.HostID
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (c *Client) handleJob(ctx context.Context, job DeploymentJob) error {
	if job.HostID != c.config.HostID {
		return fmt.Errorf("job host mismatch: %s", job.HostID)
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/agent/jobs/"+job.ID+"/start?host_id="+c.config.HostID, nil, nil); err != nil {
		return err
	}
	c.uploadLog(ctx, job.ID, "Job accepted by cplane-agent executor")

	var err error
	switch job.Action {
	case "setup_app":
		err = c.handleSetupApp(ctx, job)
	default:
		c.uploadLog(ctx, job.ID, "Action "+job.Action+" is queued, but recipe execution is not implemented yet")
	}
	if err != nil {
		c.uploadLog(ctx, job.ID, "Job failed: "+err.Error())
		failErr := c.doJSON(ctx, http.MethodPost, "/api/agent/jobs/"+job.ID+"/fail?host_id="+c.config.HostID, nil, nil)
		if failErr != nil {
			return fmt.Errorf("%w; additionally failed to mark job failed: %v", err, failErr)
		}
		return err
	}
	return c.doJSON(ctx, http.MethodPost, "/api/agent/jobs/"+job.ID+"/complete?host_id="+c.config.HostID, nil, nil)
}

func (c *Client) handleSetupApp(ctx context.Context, job DeploymentJob) error {
	var metadata SetupAppMetadata
	if strings.TrimSpace(job.MetadataJSON) == "" {
		return errors.New("setup_app metadata is required")
	}
	if err := json.Unmarshal([]byte(job.MetadataJSON), &metadata); err != nil {
		return fmt.Errorf("decode setup_app metadata: %w", err)
	}
	if metadata.AppName == "" {
		return errors.New("app_name is required")
	}
	if !filepath.IsAbs(metadata.RootPath) {
		return errors.New("root_path must be absolute")
	}
	if metadata.RecipePath == "" {
		metadata.RecipePath = filepath.Join("/opt/c-plane/apps", metadata.AppName, "deploy.yaml")
	}
	if !filepath.IsAbs(metadata.RecipePath) {
		return errors.New("recipe_path must be absolute")
	}

	initialRelease := filepath.Join(metadata.RootPath, "releases", "initial")
	sharedDir := filepath.Join(metadata.RootPath, "shared")
	currentPath := filepath.Join(metadata.RootPath, "current")
	for _, dir := range []string{metadata.RootPath, initialRelease, sharedDir, filepath.Dir(metadata.RecipePath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		c.uploadLog(ctx, job.ID, "Ensured directory "+dir)
	}
	if err := writeIfMissing(filepath.Join(initialRelease, "index.html"), []byte("<!doctype html><title>C-Plane App</title><h1>"+html.EscapeString(metadata.AppName)+"</h1>\n"), 0o644); err != nil {
		return fmt.Errorf("write initial index: %w", err)
	}
	if err := ensureSymlink(currentPath, filepath.Join("releases", "initial")); err != nil {
		return fmt.Errorf("create current symlink: %w", err)
	}
	if err := writeIfMissing(metadata.RecipePath, []byte(defaultRecipe(metadata)), 0o644); err != nil {
		return fmt.Errorf("write recipe: %w", err)
	}
	c.uploadLog(ctx, job.ID, "Prepared app root "+metadata.RootPath)
	c.uploadLog(ctx, job.ID, "Prepared recipe "+metadata.RecipePath)

	if metadata.NginxEnabled {
		if strings.TrimSpace(metadata.Domain) == "" {
			return errors.New("domain is required when nginx_enabled is true")
		}
		if err := writeNginxSite(ctx, job.ID, c.uploadLog, metadata); err != nil {
			return err
		}
	}
	return nil
}

func writeIfMissing(path string, content []byte, perm os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, content, perm)
}

func ensureSymlink(linkPath, target string) error {
	if current, err := os.Readlink(linkPath); err == nil {
		if current == target {
			return nil
		}
		return fmt.Errorf("%s already points to %s", linkPath, current)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Symlink(target, linkPath)
}

func defaultRecipe(metadata SetupAppMetadata) string {
	return fmt.Sprintf(`# C-Plane deploy recipe for %s
runtime: %s
root_path: %s
deploy:
  build: []
  publish: current
`, metadata.AppName, blank(metadata.Runtime, "static"), metadata.RootPath)
}

func writeNginxSite(ctx context.Context, jobID string, uploadLog func(context.Context, string, string), metadata SetupAppMetadata) error {
	domain := strings.TrimSpace(metadata.Domain)
	if strings.ContainsAny(domain, " /\\;") {
		return errors.New("domain contains invalid characters")
	}
	if strings.ContainsAny(metadata.RootPath, "\n\r;") {
		return errors.New("root_path contains invalid characters for nginx")
	}
	siteAvailable := filepath.Join("/etc/nginx/sites-available", domain+".conf")
	siteEnabled := filepath.Join("/etc/nginx/sites-enabled", domain+".conf")
	config := fmt.Sprintf(`server {
    listen 80;
    server_name %s;

    root %s/current;
    index index.html index.htm;

    location / {
        try_files $uri $uri/ /index.html;
    }
}
`, domain, metadata.RootPath)
	if err := os.WriteFile(siteAvailable, []byte(config), 0o644); err != nil {
		return fmt.Errorf("write nginx site %s: %w", siteAvailable, err)
	}
	if err := ensureSymlink(siteEnabled, siteAvailable); err != nil {
		return fmt.Errorf("enable nginx site: %w", err)
	}
	uploadLog(ctx, jobID, "Wrote nginx site "+siteAvailable)
	if out, err := exec.CommandContext(ctx, "nginx", "-t").CombinedOutput(); err != nil {
		return fmt.Errorf("nginx -t failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	uploadLog(ctx, jobID, "nginx -t passed")
	if out, err := exec.CommandContext(ctx, "systemctl", "reload", "nginx").CombinedOutput(); err != nil {
		return fmt.Errorf("reload nginx failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	uploadLog(ctx, jobID, "Reloaded nginx")
	return nil
}

func (c *Client) uploadLog(ctx context.Context, jobID, message string) {
	err := c.doJSON(ctx, http.MethodPost, "/api/agent/jobs/"+jobID+"/logs", map[string]string{"message": message}, nil)
	if err != nil {
		log.Printf("upload log failed: %v", err)
	}
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.config.APIURL, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "cplane-agent/"+version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token, err := readToken(c.config.TokenFile); err == nil && token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return err
		}
	}
	return nil
}

func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	config := Config{
		StateDir:     "/var/lib/c-plane-agent",
		LogDir:       "/var/log/c-plane-agent",
		TokenFile:    "/etc/c-plane/agent.token",
		PollInterval: 15 * time.Second,
	}
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch {
		case section == "" && key == "host_id":
			config.HostID = value
		case section == "" && key == "api_url":
			config.APIURL = value
		case section == "" && key == "mqtt_url":
			config.MQTTURL = value
		case section == "" && key == "state_dir":
			config.StateDir = value
		case section == "" && key == "log_dir":
			config.LogDir = value
		case section == "" && key == "poll_interval_seconds":
			seconds, err := strconv.Atoi(value)
			if err == nil && seconds > 0 {
				config.PollInterval = time.Duration(seconds) * time.Second
			}
		case section == "auth" && key == "token_file":
			config.TokenFile = value
		}
	}
	if config.HostID == "" {
		return Config{}, errors.New("host_id is required")
	}
	if config.APIURL == "" {
		return Config{}, errors.New("api_url is required")
	}
	if config.TokenFile == "" {
		return Config{}, errors.New("auth.token_file is required")
	}
	return config, nil
}

func readToken(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func writeSecret(path, value string) error {
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return err
	}
	return nil
}

func blank(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cplane-agent <register|run> --config /etc/c-plane/agent.toml")
}
