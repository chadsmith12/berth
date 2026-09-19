package launch_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chadsmith12/berth/pkg/coolify"
	"github.com/chadsmith12/berth/pkg/launch"
)

type fakeClient struct {
	projects   []coolify.Project
	details    map[string]coolify.ProjectDetail
	servers    []coolify.Server
	keys       []coolify.PrivateKey
	databases  map[string]coolify.DatabaseDetail
	createdApp []coolify.CreateApplicationBody
	envs       map[string][]coolify.EnvVar
	setVars    map[string]string
}

func (f *fakeClient) Projects(ctx context.Context) ([]coolify.Project, error) {
	return f.projects, nil
}

func (f *fakeClient) CreateProject(ctx context.Context, name string) (string, error) {
	uuid := "proj-" + name
	f.projects = append(f.projects, coolify.Project{UUID: uuid, Name: name})
	return uuid, nil
}

func (f *fakeClient) Project(ctx context.Context, uuid string) (coolify.ProjectDetail, error) {
	if d, ok := f.details[uuid]; ok {
		return d, nil
	}
	d := coolify.ProjectDetail{Project: coolify.Project{UUID: uuid}}
	f.details = map[string]coolify.ProjectDetail{}
	f.details[uuid] = d
	return d, nil
}

func (f *fakeClient) CreateEnvironment(ctx context.Context, projectUUID, name string) (string, error) {
	uuid := "env-" + name
	d := f.details[projectUUID]
	d.Environments = append(d.Environments, coolify.Environment{UUID: uuid, Name: name})
	f.details[projectUUID] = d
	return uuid, nil
}

func (f *fakeClient) Servers(ctx context.Context) ([]coolify.Server, error) {
	return f.servers, nil
}

func (f *fakeClient) PrivateKeys(ctx context.Context) ([]coolify.PrivateKey, error) {
	return f.keys, nil
}

func (f *fakeClient) CreatePrivateKey(ctx context.Context, name, pem string) (string, error) {
	uuid := "key-" + name
	f.keys = append(f.keys, coolify.PrivateKey{UUID: uuid, Name: name})
	return uuid, nil
}

func (f *fakeClient) Databases(ctx context.Context) ([]coolify.Database, error) {
	var out []coolify.Database
	for uuid, d := range f.databases {
		out = append(out, coolify.Database{UUID: uuid, Name: d.Name, Type: d.Type})
	}
	return out, nil
}

func (f *fakeClient) Database(ctx context.Context, uuid string) (coolify.DatabaseDetail, error) {
	if d, ok := f.databases[uuid]; ok {
		return d, nil
	}
	return coolify.DatabaseDetail{}, errors.New("not found")
}

func (f *fakeClient) CreateApplication(ctx context.Context, body coolify.CreateApplicationBody) (string, error) {
	f.createdApp = append(f.createdApp, body)
	return "app-1", nil
}

func (f *fakeClient) EnvVars(ctx context.Context, appUUID string) ([]coolify.EnvVar, error) {
	return f.envs[appUUID], nil
}

func (f *fakeClient) SetEnvVar(ctx context.Context, appUUID, key, value string) error {
	f.setVars[key] = value
	f.envs[appUUID] = append(f.envs[appUUID], coolify.EnvVar{UUID: "e-" + key, Key: key})
	return nil
}

func baseInputs() launch.Inputs {
	return launch.Inputs{
		ProjectName:     "pmc",
		EnvironmentName: "production",
		ServerUUID:      "srv-1",
		GitRepository:   "git@git.example.com:2222/me/app.git",
		Branch:          "main",
		PrivateKeyUUID:  "key-1",
		Domain:          "http://app.example.com",
		AppPort:         8080,
		DatabaseUUID:    "db-1",
		EnvFileName:     "docker-compose.production.yml",
	}
}

func readyClient() *fakeClient {
	return &fakeClient{
		projects: []coolify.Project{{UUID: "proj-1", Name: "pmc"}},
		details: map[string]coolify.ProjectDetail{
			"proj-1": {Project: coolify.Project{UUID: "proj-1", Name: "pmc"}, Environments: []coolify.Environment{{UUID: "env-1", Name: "production"}}},
		},
		servers: []coolify.Server{{UUID: "srv-1", Name: "localhost", IP: "10.0.0.1"}},
		keys:    []coolify.PrivateKey{{UUID: "key-1", Name: "existing"}},
		databases: map[string]coolify.DatabaseDetail{
			"db-1": {UUID: "db-1", Name: "pmc-postgres", Type: "standalone-postgresql", PostgresUser: "postgres", PostgresPassword: "secret", PostgresDB: "app"},
		},
		envs:    map[string][]coolify.EnvVar{},
		setVars: map[string]string{},
	}
}

func TestExecuteSetsMonorepoBaseDirectory(t *testing.T) {
	fc := readyClient()
	in := baseInputs()
	in.BaseDir = "web"
	_, err := launch.Execute(context.Background(), fc, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	body := fc.createdApp[0]
	if body.BaseDirectory != "/web" {
		t.Fatalf("base directory %q, want /web", body.BaseDirectory)
	}
	if body.DockerComposeLocation != "/docker-compose.production.yml" {
		t.Fatalf("compose location %q should stay base-dir-relative", body.DockerComposeLocation)
	}
}

func TestExecuteAllFound(t *testing.T) {
	fc := readyClient()
	res, err := launch.Execute(context.Background(), fc, baseInputs())
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Project.Created || res.Environment.Created || res.DeployKey.Created {
		t.Fatalf("nothing should be created: %+v", res)
	}
	if res.Application.UUID != "app-1" || !res.Application.Created {
		t.Fatalf("application: %+v", res.Application)
	}
	body := fc.createdApp[0]
	if body.BuildPack != "dockercompose" || !body.ConnectToDockerNetwork || body.AutogenerateDomain {
		t.Fatalf("body: %+v", body)
	}
	if body.DockerComposeLocation != "/docker-compose.production.yml" {
		t.Fatalf("compose location %q", body.DockerComposeLocation)
	}
	if body.BaseDirectory != "/" {
		t.Fatalf("base directory %q, want / for an app at the repo root", body.BaseDirectory)
	}
	if body.DockerComposeDomains["app"].Domain != "http://app.example.com:8080" {
		t.Fatalf("domain not derived: %+v", body.DockerComposeDomains)
	}
	if fc.setVars["DB_HOST"] != "db-1" || fc.setVars["DB_PASSWORD"] != "secret" {
		t.Fatalf("db vars: %+v", fc.setVars)
	}
	if fc.setVars["APP_KEY"] == "" || !strings.HasPrefix(fc.setVars["APP_KEY"], "base64:") {
		t.Fatalf("APP_KEY: %q", fc.setVars["APP_KEY"])
	}
}

func TestExecuteRefusesDuplicateEnvironmentName(t *testing.T) {
	fc := readyClient()
	fc.details["proj-1"] = coolify.ProjectDetail{
		Project: coolify.Project{UUID: "proj-1", Name: "pmc"},
		Environments: []coolify.Environment{
			{UUID: "env-a", Name: "production"},
			{UUID: "env-b", Name: "production"},
		},
	}
	_, err := launch.Execute(context.Background(), fc, baseInputs())
	if err == nil || !strings.Contains(err.Error(), "rename one") {
		t.Fatalf("got %v", err)
	}
}

func TestExecuteCreatesMissingProjectAndEnv(t *testing.T) {
	fc := readyClient()
	fc.projects = nil
	fc.details = map[string]coolify.ProjectDetail{}
	in := baseInputs()
	res, err := launch.Execute(context.Background(), fc, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !res.Project.Created || res.Project.Name != "pmc" {
		t.Fatalf("project: %+v", res.Project)
	}
	if !res.Environment.Created {
		t.Fatalf("environment: %+v", res.Environment)
	}
}

func TestExecuteRefusesDuplicateProjectName(t *testing.T) {
	fc := readyClient()
	fc.projects = append(fc.projects, coolify.Project{UUID: "proj-2", Name: "pmc"})
	_, err := launch.Execute(context.Background(), fc, baseInputs())
	if err == nil || !strings.Contains(err.Error(), "rename one") {
		t.Fatalf("got %v", err)
	}
}

func TestExecuteUnknownServerListsServers(t *testing.T) {
	fc := readyClient()
	in := baseInputs()
	in.ServerUUID = "nope"
	_, err := launch.Execute(context.Background(), fc, in)
	if err == nil || !strings.Contains(err.Error(), "localhost (srv-1)") {
		t.Fatalf("got %v", err)
	}
}

func TestExecuteCreatesDeployKey(t *testing.T) {
	fc := readyClient()
	fc.keys = nil
	in := baseInputs()
	in.PrivateKeyUUID = ""
	in.NewKeyName = "pmc-production-deploy-key"
	res, err := launch.Execute(context.Background(), fc, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !res.DeployKey.Created || res.DeployKey.UUID == "" {
		t.Fatalf("deploy key: %+v", res.DeployKey)
	}
}

func TestExecuteUnknownDeployKeyListsKeys(t *testing.T) {
	fc := readyClient()
	in := baseInputs()
	in.PrivateKeyUUID = "nope"
	_, err := launch.Execute(context.Background(), fc, in)
	if err == nil || !strings.Contains(err.Error(), "existing (key-1)") {
		t.Fatalf("got %v", err)
	}
}

func TestExecuteDomainPortMismatch(t *testing.T) {
	fc := readyClient()
	in := baseInputs()
	in.Domain = "http://app.example.com:9090"
	_, err := launch.Execute(context.Background(), fc, in)
	if err == nil || !strings.Contains(err.Error(), "9090") || !strings.Contains(err.Error(), "8080") {
		t.Fatalf("got %v", err)
	}
}

// An https domain is preserved with the port appended: the scheme drives TLS in
// Coolify, so berth must not downgrade it to http.
func TestExecuteHttpsDomainPreserved(t *testing.T) {
	fc := readyClient()
	in := baseInputs()
	in.Domain = "https://app.example.com"
	if _, err := launch.Execute(context.Background(), fc, in); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := fc.createdApp[0].DockerComposeDomains["app"].Domain; got != "https://app.example.com:8080" {
		t.Fatalf("domain %q, want https preserved", got)
	}
}

func TestExecuteUnsupportedSchemeRejected(t *testing.T) {
	fc := readyClient()
	in := baseInputs()
	in.Domain = "ftp://app.example.com"
	_, err := launch.Execute(context.Background(), fc, in)
	if err == nil || !strings.Contains(err.Error(), "ftp") || !strings.Contains(err.Error(), "http:// or https://") {
		t.Fatalf("got %v", err)
	}
}

func TestExecuteDatabaseCredentialsRequireSensitive(t *testing.T) {
	fc := readyClient()
	db := fc.databases["db-1"]
	db.PostgresPassword = ""
	fc.databases["db-1"] = db
	_, err := launch.Execute(context.Background(), fc, baseInputs())
	if err == nil || !strings.Contains(err.Error(), "read:sensitive") {
		t.Fatalf("got %v", err)
	}
}

func TestExecuteRedisVars(t *testing.T) {
	fc := readyClient()
	fc.databases["redis-1"] = coolify.DatabaseDetail{UUID: "redis-1", Name: "pmc-redis", Type: "standalone-redis", RedisPassword: "redispass"}
	in := baseInputs()
	in.RedisUUID = "redis-1"
	res, err := launch.Execute(context.Background(), fc, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fc.setVars["REDIS_HOST"] != "redis-1" || fc.setVars["REDIS_PORT"] != "6379" || fc.setVars["REDIS_PASSWORD"] != "redispass" {
		t.Fatalf("redis vars: %+v", fc.setVars)
	}
	_ = res
}

func TestExecuteDatabaseNameNotice(t *testing.T) {
	fc := readyClient()
	in := baseInputs()
	in.DatabaseName = "app2"
	res, err := launch.Execute(context.Background(), fc, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fc.setVars["DB_DATABASE"] != "app2" {
		t.Fatalf("DB_DATABASE %q", fc.setVars["DB_DATABASE"])
	}
	if len(res.Notices) != 1 || !strings.Contains(res.Notices[0], "does not exist") {
		t.Fatalf("notices %+v", res.Notices)
	}

	// the resource's own name produces no notice
	in.DatabaseName = "app"
	res, err = launch.Execute(context.Background(), fc, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(res.Notices) != 0 {
		t.Fatalf("notices %+v", res.Notices)
	}
}

func TestExecuteRedisTypeMismatch(t *testing.T) {
	fc := readyClient()
	in := baseInputs()
	in.RedisUUID = "db-1"
	_, err := launch.Execute(context.Background(), fc, in)
	if err == nil || !strings.Contains(err.Error(), "not redis") {
		t.Fatalf("got %v", err)
	}
}

func TestExecuteNoDatabase(t *testing.T) {
	fc := readyClient()
	in := baseInputs()
	in.DatabaseUUID = ""
	res, err := launch.Execute(context.Background(), fc, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fc.setVars["DB_HOST"] != "" {
		t.Fatalf("db vars must be absent: %+v", fc.setVars)
	}
	_ = res
}
