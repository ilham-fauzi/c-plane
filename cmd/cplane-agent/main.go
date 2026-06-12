package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	ID        string `json:"id"`
	AppID     string `json:"app_id"`
	HostID    string `json:"host_id"`
	Action    string `json:"action"`
	Status    string `json:"status"`
	ReleaseID string `json:"release_id,omitempty"`
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
	c.uploadLog(ctx, job.ID, "Job accepted by cplane-agent MVP executor")
	c.uploadLog(ctx, job.ID, "Action "+job.Action+" is not executing recipes yet")
	return c.doJSON(ctx, http.MethodPost, "/api/agent/jobs/"+job.ID+"/complete?host_id="+c.config.HostID, nil, nil)
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

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cplane-agent <register|run> --config /etc/c-plane/agent.toml")
}
