package sdk

import (
	"database/sql/driver"
	json "encoding/json"
	"fmt"
	"strings"
	"time"
)

type ProjectIdentifiers struct {
	ID  int64  `json:"-" yaml:"-" db:"id"`
	Key string `json:"-" yaml:"-" db:"projectkey"`
}

type Projects []Project

func (projects Projects) Keys() []string {
	var res = make([]string, len(projects))
	for i := range projects {
		res[i] = projects[i].Key
	}
	return res
}

// Project represent a team with group of users and pipelines
type Project struct {
	ID           int64     `json:"-" yaml:"-" db:"id" cli:"-"`
	Key          string    `json:"key" yaml:"key" db:"projectkey" cli:"key,key" action_metadata:"project-key"`
	Name         string    `json:"name" yaml:"name" db:"name" cli:"name"`
	Description  string    `json:"description" yaml:"description" db:"description" cli:"description"`
	Icon         string    `json:"icon" yaml:"icon" db:"icon" cli:"-"`
	Created      time.Time `json:"created" yaml:"created" db:"created" `
	LastModified time.Time `json:"last_modified" yaml:"last_modified" db:"last_modified"`
	// aggregates
	Workflows        []Workflow           `json:"workflows,omitempty" yaml:"workflows,omitempty" db:"-" cli:"-"`
	WorkflowNames    IDNames              `json:"workflow_names,omitempty" yaml:"workflow_names,omitempty" db:"-" cli:"-"`
	Pipelines        []Pipeline           `json:"pipelines,omitempty" yaml:"pipelines,omitempty" db:"-"  cli:"-"`
	PipelineNames    IDNames              `json:"pipeline_names,omitempty" yaml:"pipeline_names,omitempty" db:"-"  cli:"-"`
	Applications     []Application        `json:"applications,omitempty" yaml:"applications,omitempty" db:"-"  cli:"-"`
	ApplicationNames IDNames              `json:"application_names,omitempty" yaml:"application_names,omitempty" db:"-"  cli:"-"`
	ProjectGroups    GroupPermissions     `json:"groups,omitempty" yaml:"permissions,omitempty" db:"-"  cli:"-"`
	Variables        []ProjectVariable    `json:"variables,omitempty" yaml:"variables,omitempty" db:"-"  cli:"-"`
	Environments     []Environment        `json:"environments,omitempty" yaml:"environments,omitempty" db:"-"  cli:"-"`
	EnvironmentNames IDNames              `json:"environment_names,omitempty" yaml:"environment_names,omitempty" db:"-"  cli:"-"`
	Labels           []Label              `json:"labels,omitempty" yaml:"labels,omitempty" db:"-"  cli:"-"`
	Metadata         Metadata             `json:"metadata" yaml:"metadata" db:"metadata" cli:"-"`
	Keys             []ProjectKey         `json:"keys,omitempty" yaml:"keys" db:"-" cli:"-"`
	VCSServers       []VCSProject         `json:"vcs_servers" yaml:"vcs_servers" db:"-" cli:"-"`
	Integrations     []ProjectIntegration `json:"integrations" yaml:"integrations" db:"-" cli:"-"`
	Features         map[string]bool      `json:"features" yaml:"features" db:"-" cli:"-"`
	URLs             URL                  `json:"urls" yaml:"-" db:"-" cli:"-"`
	Organization     string               `json:"organization" yaml:"-" db:"-" cli:"-"`
	// fields used by UI
	Permissions struct {
		Writable bool `json:"writable"`
	} `json:"permissions" yaml:"-" db:"-"  cli:"-"`
}

type GroupPermissions []GroupPermission

func (g GroupPermissions) ComputeOrganization() (string, error) {
	var org string
	for _, gp := range g {
		if gp.Permission <= PermissionRead {
			continue
		}
		if gp.Group.Organization == "" {
			continue
		}
		if org != "" && gp.Group.Organization != org {
			return "", NewErrorFrom(ErrForbidden, "group permissions organization conflict")
		}
		org = gp.Group.Organization
	}
	return org, nil
}

func (g GroupPermissions) GetByGroupID(groupID int64) *GroupPermission {
	for i := range g {
		if g[i].Group.ID == groupID {
			return &g[i]
		}
	}
	return nil
}

type Permissions struct {
	Readable   bool `json:"readable"`
	Writable   bool `json:"writable"`
	Executable bool `json:"executable"`
}

func (p Permissions) Level() int {
	var i = 7
	if !p.Writable {
		i = 5
	}
	if !p.Executable {
		i = 4
	}
	if !p.Readable {
		i = 0
	}
	return i
}

// IsMaxLevel returns true if permissions has level 7 (writable + readable + executable)
func (p Permissions) IsMaxLevel() bool {
	return p.Level() == 7
}

type URL struct {
	APIURL string `json:"api_url,omitempty"`
	UIURL  string `json:"ui_url,omitempty"`
}

// SetApplication data on project
func (proj *Project) SetApplication(app Application) {
	found := false
	for i, a := range proj.Applications {
		if a.Name == app.Name {
			proj.Applications[i] = app
			found = true
			break
		}
	}
	if !found {
		proj.Applications = append(proj.Applications, app)
	}
}

// SetEnvironment data on project
func (proj *Project) SetEnvironment(env Environment) {
	found := false
	for i, e := range proj.Environments {
		if e.Name == env.Name {
			proj.Environments[i] = env
			found = true
			break
		}
	}
	if !found {
		proj.Environments = append(proj.Environments, env)
	}
}

// SetPipeline data on project
func (proj *Project) SetPipeline(pip Pipeline) {
	found := false
	for i, p := range proj.Pipelines {
		if p.Name == pip.Name {
			proj.Pipelines[i] = pip
			found = true
			break
		}
	}
	if !found {
		proj.Pipelines = append(proj.Pipelines, pip)
	}
}

// IsValid returns error if the project is not valid.
func (proj Project) IsValid() error {
	if !NamePatternRegex.MatchString(proj.Key) {
		return NewError(ErrInvalidName, fmt.Errorf("Invalid project key. It should match %s", NamePattern))
	}

	if proj.Icon != "" {
		if !strings.HasPrefix(proj.Icon, IconFormat) {
			return ErrIconBadFormat
		}
		if len(proj.Icon) > MaxIconSize {
			return ErrIconBadSize
		}
	}

	return nil
}

// GetSSHKey returns a ssh key given his name
func (proj Project) GetSSHKey(name string) *ProjectKey {
	for _, k := range proj.Keys {
		if k.Type == KeyTypeSSH && k.Name == name {
			return &k
		}
	}
	return nil
}

// SSHKeys returns the slice of ssh key for an application
func (proj Project) SSHKeys() []ProjectKey {
	keys := []ProjectKey{}
	for _, k := range proj.Keys {
		if k.Type == KeyTypeSSH {
			keys = append(keys, k)
		}
	}
	return keys
}

// PGPKeys returns the slice of pgp key for a project
func (proj Project) PGPKeys() []ProjectKey {
	keys := []ProjectKey{}
	for _, k := range proj.Keys {
		if k.Type == KeyTypePGP {
			keys = append(keys, k)
		}
	}
	return keys
}

// GetIntegration returns the ProjectIntegration given a name
func (proj Project) GetIntegration(pfName string) (ProjectIntegration, bool) {
	for i := range proj.Integrations {
		if proj.Integrations[i].Name == pfName {
			return proj.Integrations[i], true
		}
	}
	return ProjectIntegration{}, false
}

// GetIntegrationByID returns the ProjectIntegration given a name
func (proj Project) GetIntegrationByID(id int64) *ProjectIntegration {
	for i := range proj.Integrations {
		if proj.Integrations[i].ID == id {
			return &proj.Integrations[i]
		}
	}
	return nil
}

// ProjectVariableAudit represents an audit on a project variable
type ProjectVariableAudit struct {
	ID             int64            `json:"id" yaml:"-" db:"id"`
	ProjectID      int64            `json:"project_id" yaml:"-" db:"project_id"`
	VariableID     int64            `json:"variable_id" yaml:"-" db:"variable_id"`
	Type           string           `json:"type" yaml:"-" db:"type"`
	VariableBefore *ProjectVariable `json:"variable_before,omitempty" yaml:"-" db:"-"`
	VariableAfter  ProjectVariable  `json:"variable_after,omitempty" yaml:"-" db:"-"`
	Versionned     time.Time        `json:"versionned" yaml:"-" db:"versionned"`
	Author         string           `json:"author" yaml:"-" db:"author"`
}

// Metadata represents metadata
type Metadata map[string]string

// Value returns driver.Value from Metadata.
func (a Metadata) Value() (driver.Value, error) {
	j, err := json.Marshal(a)
	return j, WrapError(err, "cannot marshal Metadata")
}

// Scan Metadata.
func (a *Metadata) Scan(src interface{}) error {
	if src == nil {
		return nil
	}
	source, ok := src.([]byte)
	if !ok {
		return WithStack(fmt.Errorf("type assertion .([]byte) failed (%T)", src))
	}
	return WrapError(JSONUnmarshal(source, a), "cannot unmarshal Metadata")
}

// LastModification is stored in cache and used for ProjectLastUpdates computing
type LastModification struct {
	Key          string `json:"key,omitempty"`
	Name         string `json:"name"`
	Username     string `json:"username"`
	LastModified int64  `json:"last_modified"`
	Type         string `json:"type,omitempty"`
}

const (
	// ApplicationLastModificationType represent key for last update event about application
	ApplicationLastModificationType = "application"
	// PipelineLastModificationType represent key for last update event about pipeline
	PipelineLastModificationType = "pipeline"
	// WorkflowLastModificationType represent key for last update event about workflow
	WorkflowLastModificationType = "workflow"
	// ProjectLastModificationType represent key for last update event about project
	ProjectLastModificationType = "project"
	// ProjectPipelineLastModificationType represent key for last update event about project.pipeline (rename, delete or add a pipeline)
	ProjectPipelineLastModificationType = "project.pipeline"
	// ProjectApplicationLastModificationType represent key for last update event about project.application (rename, delete or add an application)
	ProjectApplicationLastModificationType = "project.application"
	// ProjectEnvironmentLastModificationType represent key for last update event about project.environment (rename, delete or add an environment)
	ProjectEnvironmentLastModificationType = "project.environment"
	// ProjectWorkflowLastModificationType represent key for last update event about project.workflow (rename, delete or add a workflow)
	ProjectWorkflowLastModificationType = "project.workflow"
	// ProjectVariableLastModificationType represent key for last update event about project.variable (rename, delete or add a variable)
	ProjectVariableLastModificationType = "project.variable"
	// ProjectKeysLastModificationType represent key for last update event about project.keys (add, delete a key)
	ProjectKeysLastModificationType = "project.keys"
	// ProjectIntegrationsLastModificationType represent key for last update event about project.integrations (add, update, delete a integration)
	ProjectIntegrationsLastModificationType = "project.integrations"
)

// ProjectLastUpdates update times of project, application and pipelines
// Deprecated
type ProjectLastUpdates struct {
	LastModification
	Applications []LastModification `json:"applications"`
	Pipelines    []LastModification `json:"pipelines"`
	Environments []LastModification `json:"environments"`
	Workflows    []LastModification `json:"workflows"`
}

// ProjectKeyPattern  pattern for project key
const ProjectKeyPattern = "^[A-Z0-9]{1,}$"

// ProjectsToIDs returns ids of given projects.
func ProjectsToIDs(ps []Project) []int64 {
	ids := make([]int64, len(ps))
	for i := range ps {
		ids[i] = ps[i].ID
	}
	return ids
}
