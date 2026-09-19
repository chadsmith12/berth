package coolify

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Project struct {
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Environment struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type ProjectDetail struct {
	Project
	Environments []Environment `json:"environments"`
}

// Projects lists the projects the token can see.
func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	var projects []Project
	if err := c.get(ctx, "/projects", &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

// CreateProject creates a project and returns its uuid.
func (c *Client) CreateProject(ctx context.Context, name string) (string, error) {
	var res struct {
		UUID string `json:"uuid"`
	}
	if err := c.post(ctx, "/projects", map[string]string{"name": name}, &res); err != nil {
		return "", err
	}
	return res.UUID, nil
}

// Project fetches one project with its environments.
func (c *Client) Project(ctx context.Context, uuid string) (ProjectDetail, error) {
	var p ProjectDetail
	if err := c.get(ctx, "/projects/"+uuid, &p); err != nil {
		return ProjectDetail{}, err
	}
	return p, nil
}

// CreateEnvironment creates an environment inside a project and returns its uuid.
func (c *Client) CreateEnvironment(ctx context.Context, projectUUID, name string) (string, error) {
	var res struct {
		UUID string `json:"uuid"`
	}
	path := fmt.Sprintf("/projects/%s/environments", projectUUID)
	if err := c.post(ctx, path, map[string]string{"name": name}, &res); err != nil {
		return "", err
	}
	return res.UUID, nil
}

type Server struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// Servers lists the servers the token can see.
func (c *Client) Servers(ctx context.Context) ([]Server, error) {
	var servers []Server
	if err := c.get(ctx, "/servers", &servers); err != nil {
		return nil, err
	}
	return servers, nil
}

type PrivateKey struct {
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	Fingerprint  string `json:"fingerprint"`
	IsGitRelated bool   `json:"is_git_related"`
}

// PrivateKeys lists the stored private keys.
func (c *Client) PrivateKeys(ctx context.Context) ([]PrivateKey, error) {
	var keys []PrivateKey
	if err := c.get(ctx, "/security/keys", &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// CreatePrivateKey stores a private key and returns its uuid.
func (c *Client) CreatePrivateKey(ctx context.Context, name, privateKeyPEM string) (string, error) {
	var res struct {
		UUID string `json:"uuid"`
	}
	body := map[string]string{"name": name, "private_key": privateKeyPEM}
	if err := c.post(ctx, "/security/keys", body, &res); err != nil {
		return "", err
	}
	return res.UUID, nil
}

type Database struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Type string `json:"database_type"`
}

// Databases lists the database resources (postgres, redis, ...).
func (c *Client) Databases(ctx context.Context) ([]Database, error) {
	var dbs []Database
	if err := c.get(ctx, "/databases", &dbs); err != nil {
		return nil, err
	}
	return dbs, nil
}

// DatabaseDetail holds what a compose stack needs to reach a database. The
// password fields are only present for tokens with the read:sensitive
// ability; without it Coolify omits them from an otherwise-200 response.
type DatabaseDetail struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Type string `json:"database_type"`

	PostgresUser     string `json:"postgres_user"`
	PostgresPassword string `json:"postgres_password"`
	PostgresDB       string `json:"postgres_db"`

	RedisUsername string `json:"redis_username"`
	RedisPassword string `json:"redis_password"`
}

// Database fetches one database with its connection details.
func (c *Client) Database(ctx context.Context, uuid string) (DatabaseDetail, error) {
	var d DatabaseDetail
	if err := c.get(ctx, "/databases/"+uuid, &d); err != nil {
		return DatabaseDetail{}, err
	}
	return d, nil
}

// CreateApplicationBody is the payload for POST /applications/private-deploy-key.
// Coolify requires the scp-style repository form git@host:port/path and
// rejects ssh:// URLs; each docker_compose_domains entry needs a name.
type CreateApplicationBody struct {
	ProjectUUID            string                       `json:"project_uuid"`
	EnvironmentName        string                       `json:"environment_name"`
	ServerUUID             string                       `json:"server_uuid"`
	GitRepository          string                       `json:"git_repository"`
	GitBranch              string                       `json:"git_branch"`
	PrivateKeyUUID         string                       `json:"private_key_uuid"`
	BuildPack              string                       `json:"build_pack"`
	BaseDirectory          string                       `json:"base_directory"`
	DockerComposeLocation  string                       `json:"docker_compose_location"`
	ConnectToDockerNetwork bool                         `json:"connect_to_docker_network"`
	AutogenerateDomain     bool                         `json:"autogenerate_domain"`
	Name                   string                       `json:"name"`
	DockerComposeDomains   map[string]ComposeDomainBody `json:"docker_compose_domains"`
}

type ComposeDomainBody struct {
	Domain string `json:"domain"`
	Name   string `json:"name"`
}

// CreateApplication creates a docker-compose application from a private
// deploy key and returns its uuid.
func (c *Client) CreateApplication(ctx context.Context, body CreateApplicationBody) (string, error) {
	var res struct {
		UUID string `json:"uuid"`
	}
	if err := c.post(ctx, "/applications/private-deploy-key", body, &res); err != nil {
		return "", err
	}
	return res.UUID, nil
}

type Application struct {
	UUID                  string `json:"uuid"`
	Name                  string `json:"name"`
	Fqdn                  string `json:"fqdn"`
	Status                string `json:"status"` // "<state>:<health>", e.g. "running:unknown"
	GitRepository         string `json:"git_repository"`
	GitBranch             string `json:"git_branch"`
	DockerComposeLocation string `json:"docker_compose_location"`
	DockerComposeDomains  string `json:"docker_compose_domains"`
}

// ComposeDomains decodes the docker_compose_domains blob — Coolify returns
// it as a JSON-encoded object mapping service name to domain, in a string.
// Empty or malformed blobs are no domains; compose applications leave Fqdn
// empty and keep their domains here.
func (a Application) ComposeDomains() []string {
	if a.DockerComposeDomains == "" {
		return nil
	}
	var entries map[string]struct {
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal([]byte(a.DockerComposeDomains), &entries); err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var domains []string
	for _, name := range names {
		if d := entries[name].Domain; d != "" {
			domains = append(domains, d)
		}
	}
	return domains
}

// Application fetches one application. A 404 here may mean the token is
// scoped to another team; the error names the team.
func (c *Client) Application(ctx context.Context, uuid string) (Application, error) {
	var a Application
	if err := c.get(ctx, "/applications/"+uuid, &a); err != nil {
		return Application{}, err
	}
	return a, nil
}

type EnvVar struct {
	UUID string `json:"uuid"`
	Key  string `json:"key"`
}

// EnvVars lists an application's environment variable keys.
func (c *Client) EnvVars(ctx context.Context, appUUID string) ([]EnvVar, error) {
	var envs []EnvVar
	if err := c.get(ctx, "/applications/"+appUUID+"/envs", &envs); err != nil {
		return nil, err
	}
	return envs, nil
}

// CreateEnvVar sets one environment variable.
func (c *Client) CreateEnvVar(ctx context.Context, appUUID, key, value string) error {
	body := map[string]string{"key": key, "value": value}
	return c.post(ctx, "/applications/"+appUUID+"/envs", body, nil)
}

// UpdateEnvVar replaces an existing environment variable's value.
func (c *Client) UpdateEnvVar(ctx context.Context, appUUID, key, value string) error {
	body := map[string]string{"key": key, "value": value}
	return c.patch(ctx, "/applications/"+appUUID+"/envs", body, nil)
}

// SetEnvVar creates or updates one environment variable.
func (c *Client) SetEnvVar(ctx context.Context, appUUID, key, value string) error {
	envs, err := c.EnvVars(ctx, appUUID)
	if err != nil {
		return err
	}
	for _, e := range envs {
		if e.Key == key {
			return c.UpdateEnvVar(ctx, appUUID, key, value)
		}
	}
	return c.CreateEnvVar(ctx, appUUID, key, value)
}

// DeployApplication triggers a deployment for an application and returns the
// queued deployment's uuid. The response wraps an array — Coolify may queue
// several resources at once; berth takes the first.
func (c *Client) DeployApplication(ctx context.Context, appUUID string) (string, error) {
	var res struct {
		Deployments []struct {
			DeploymentUUID string `json:"deployment_uuid"`
			Message        string `json:"message"`
		} `json:"deployments"`
	}
	if err := c.post(ctx, "/deploy?uuid="+appUUID, nil, &res); err != nil {
		return "", err
	}
	if len(res.Deployments) == 0 {
		return "", fmt.Errorf("no deployment queued for %s", appUUID)
	}
	return res.Deployments[0].DeploymentUUID, nil
}

// Deployment is one application deployment record. Logs arrive as a
// JSON-encoded array of entries; entries with hidden=true are Coolify's
// internal commands (helper containers, docker plumbing) rather than build
// output. Older Coolify versions omit the field entirely.
type Deployment struct {
	DeploymentUUID string `json:"deployment_uuid"`
	Status         string `json:"status"`
	Logs           string `json:"logs"`
	Commit         string `json:"commit"`
	DeploymentURL  string `json:"deployment_url"` // path part of the Coolify UI page
	CreatedAt      string `json:"created_at"`
	FinishedAt     string `json:"finished_at"`
}

type DeploymentLog struct {
	Command   *string `json:"command"`
	Output    string  `json:"output"`
	Type      string  `json:"type"`
	Timestamp string  `json:"timestamp"`
	Hidden    bool    `json:"hidden"`
	Batch     int     `json:"batch"`
	Order     int     `json:"order"`
}

// LogEntries decodes the deployment's log blob. Empty or malformed blobs are
// simply no entries — older versions have no logs at all.
func (d Deployment) LogEntries() []DeploymentLog {
	if d.Logs == "" {
		return nil
	}
	var entries []DeploymentLog
	if err := json.Unmarshal([]byte(d.Logs), &entries); err != nil {
		return nil
	}
	return entries
}

// VisibleLogs returns the build output lines: non-hidden entries in order.
// This is the regular build log; the hidden entries are debug noise.
func (d Deployment) VisibleLogs() []string {
	var lines []string
	for _, e := range d.LogEntries() {
		if e.Hidden {
			continue
		}
		for _, line := range strings.Split(strings.TrimRight(e.Output, "\n"), "\n") {
			if line != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

// Deployment fetches one deployment record.
func (c *Client) Deployment(ctx context.Context, uuid string) (Deployment, error) {
	var d Deployment
	if err := c.get(ctx, "/deployments/"+uuid, &d); err != nil {
		return Deployment{}, err
	}
	return d, nil
}

// ApplicationDeployments lists an application's deployment records, newest
// first. Each record has the same shape GET /deployments/{uuid} returns.
func (c *Client) ApplicationDeployments(ctx context.Context, appUUID string) ([]Deployment, error) {
	var res struct {
		Count       int          `json:"count"`
		Deployments []Deployment `json:"deployments"`
	}
	if err := c.get(ctx, "/deployments/applications/"+appUUID, &res); err != nil {
		return nil, err
	}
	return res.Deployments, nil
}

// ApplicationLogs returns the running application's recent container logs.
// Coolify rejects the request while the application is not running.
func (c *Client) ApplicationLogs(ctx context.Context, appUUID string) (string, error) {
	var res struct {
		Logs string `json:"logs"`
	}
	if err := c.get(ctx, "/applications/"+appUUID+"/logs", &res); err != nil {
		return "", err
	}
	return res.Logs, nil
}

func (c *Client) patch(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, "PATCH", path, body, out)
}
