package api

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-gorp/gorp"
	"github.com/gorilla/mux"
	"github.com/rockbears/log"

	"github.com/ovh/cds/engine/api/authentication"
	"github.com/ovh/cds/engine/api/event"
	"github.com/ovh/cds/engine/api/event_v2"
	"github.com/ovh/cds/engine/api/group"
	"github.com/ovh/cds/engine/api/integration"
	"github.com/ovh/cds/engine/api/keys"
	"github.com/ovh/cds/engine/api/permission"
	"github.com/ovh/cds/engine/api/project"
	"github.com/ovh/cds/engine/api/user"
	"github.com/ovh/cds/engine/api/worker"
	"github.com/ovh/cds/engine/api/workflow"
	"github.com/ovh/cds/engine/service"
	"github.com/ovh/cds/sdk"
	"github.com/ovh/cds/sdk/slug"
)

func (api *API) getProjectsHandler_FilterByRepo(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	withPermissions := r.FormValue("permission")
	filterByRepo := r.FormValue("repo")

	var projects sdk.Projects
	var err error
	var filterByRepoFunc = func(ctx context.Context, db gorp.SqlExecutor, p *sdk.Project) error {
		//Filter the applications by repo
		apps := []sdk.Application{}
		for i := range p.Applications {
			if p.Applications[i].RepositoryFullname == filterByRepo {
				apps = append(apps, p.Applications[i])
			}
		}
		p.Applications = apps
		ws := []sdk.Workflow{}
		//Filter the workflow by applications
		for i := range p.Workflows {
			w, err := workflow.LoadByID(ctx, db, api.Cache, *p, p.Workflows[i].ID, workflow.LoadOptions{})
			if err != nil {
				return err
			}

			//Checks the workflow use one of the applications
		wapps:
			for _, a := range w.Applications {
				for _, b := range apps {
					if a.Name == b.Name {
						ws = append(ws, p.Workflows[i])
						break wapps
					}
				}
			}
		}
		p.Workflows = ws
		return nil
	}

	opts := []project.LoadOptionFunc{
		project.LoadOptions.WithApplications,
		project.LoadOptions.WithWorkflows,
	}
	opts = append(opts, filterByRepoFunc)

	if isMaintainer(ctx) {
		projects, err = project.LoadAllByRepo(ctx, api.mustDB(), api.Cache, filterByRepo, opts...)
		if err != nil {
			return err
		}
	} else {
		projects, err = project.LoadAllByRepoAndGroupIDs(ctx, api.mustDB(), getUserConsumer(ctx).GetGroupIDs(), filterByRepo, opts...)
		if err != nil {
			return err
		}
	}

	pKeys := projects.Keys()
	perms, err := permission.LoadProjectMaxLevelPermission(ctx, api.mustDB(), pKeys, getUserConsumer(ctx).GetGroupIDs())
	if err != nil {
		return err
	}
	for i := range projects {
		if isAdmin(ctx) {
			projects[i].Permissions.Writable = true
			continue
		}
		projects[i].Permissions.Writable = perms[projects[i].Key].Writable
	}

	if strings.ToUpper(withPermissions) == "W" {
		res := make([]sdk.Project, 0, len(projects))
		for _, p := range projects {
			if p.Permissions.Writable {
				res = append(res, p)
			}
		}
		projects = res
	}

	return service.WriteJSON(w, projects, http.StatusOK)
}

func (api *API) getProjectsHandler() service.Handler {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
		withPermissions := r.FormValue("permission")
		filterByRepo := r.FormValue("repo")
		if filterByRepo != "" {
			return api.getProjectsHandler_FilterByRepo(ctx, w, r)
		}

		withApplications := service.FormBool(r, "application")
		withWorkflows := service.FormBool(r, "workflow")

		requestedUserName := r.Header.Get("X-Cds-Username")
		var requestedUser *sdk.AuthentifiedUser
		if requestedUserName != "" {
			var err error
			requestedUser, err = user.LoadByUsername(ctx, api.mustDB(), requestedUserName)
			if err != nil {
				if sdk.Cause(err) == sql.ErrNoRows {
					return sdk.WithStack(sdk.ErrUserNotFound)
				}
				return sdk.WrapError(err, "unable to load user '%s'", requestedUserName)
			}
		}

		var opts []project.LoadOptionFunc
		if withApplications {
			opts = append(opts, project.LoadOptions.WithApplications)
		}
		if withWorkflows {
			opts = append(opts, project.LoadOptions.WithIntegrations, project.LoadOptions.WithWorkflows)
		}

		var projects sdk.Projects
		var err error
		switch {
		case isMaintainer(ctx) && requestedUser == nil:
			projects, err = project.LoadAll(ctx, api.mustDB(), api.Cache, opts...)
		case isMaintainer(ctx) && requestedUser != nil:
			groups, errG := group.LoadAllByUserID(context.TODO(), api.mustDB(), requestedUser.ID)
			if errG != nil {
				return sdk.WrapError(errG, "unable to load user '%s' groups", requestedUserName)
			}
			requestedUser.Groups = groups
			log.Debug(ctx, "load all projects for user %s", requestedUser.Fullname)
			projects, err = project.LoadAllByGroupIDs(ctx, api.mustDB(), api.Cache, requestedUser.GetGroupIDs(), opts...)
		default:
			projects, err = project.LoadAllByGroupIDs(ctx, api.mustDB(), api.Cache, getUserConsumer(ctx).GetGroupIDs(), opts...)
		}
		if err != nil {
			return err
		}

		var groupIDs []int64
		var admin bool
		if requestedUser == nil {
			groupIDs = getUserConsumer(ctx).GetGroupIDs()
			admin = isAdmin(ctx)
		} else {
			groupIDs = requestedUser.GetGroupIDs()
			admin = requestedUser.Ring == sdk.UserRingAdmin
		}

		pKeys := projects.Keys()
		perms, err := permission.LoadProjectMaxLevelPermission(ctx, api.mustDB(), pKeys, groupIDs)
		if err != nil {
			return err
		}
		for i := range projects {
			if admin {
				projects[i].Permissions.Writable = true
				continue
			}
			projects[i].Permissions.Writable = perms[projects[i].Key].Writable
		}

		if strings.ToUpper(withPermissions) == "W" {
			res := make([]sdk.Project, 0, len(projects))
			for _, p := range projects {
				if p.Permissions.Writable {
					res = append(res, p)
				}
			}
			projects = res
		}

		return service.WriteJSON(w, projects, http.StatusOK)
	}
}

func (api *API) updateProjectHandler() service.Handler {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
		// Get project name in URL
		vars := mux.Vars(r)
		key := vars[permProjectKey]

		u := getUserConsumer(ctx)
		if u == nil {
			return sdk.WithStack(sdk.ErrForbidden)
		}

		proj := &sdk.Project{}
		if err := service.UnmarshalBody(r, proj); err != nil {
			return sdk.WrapError(err, "Unmarshall error")
		}

		if proj.Name == "" {
			return sdk.WrapError(sdk.ErrInvalidProjectName, "updateProject> Project name must no be empty")
		}

		// Check Request
		if key != proj.Key {
			return sdk.WrapError(sdk.ErrWrongRequest, "updateProject> bad Project key %s/%s ", key, proj.Key)
		}
		// Check is project exist
		p, errProj := project.Load(ctx, api.mustDB(), key)
		if errProj != nil {
			return sdk.WrapError(errProj, "updateProject> Cannot load project from db")
		}
		// Update in DB is made given the primary key
		proj.ID = p.ID
		proj.VCSServers = p.VCSServers
		if proj.Icon == "" {
			p.Icon = proj.Icon
		}
		if errUp := project.Update(api.mustDB(), proj); errUp != nil {
			return sdk.WrapError(errUp, "updateProject> Cannot update project %s", key)
		}
		event.PublishUpdateProject(ctx, proj, p, getUserConsumer(ctx))
		event_v2.PublishProjectEvent(ctx, api.Cache, sdk.EventProjectUpdated, *proj, *u.AuthConsumerUser.AuthentifiedUser)

		proj.Permissions.Writable = true

		return service.WriteJSON(w, proj, http.StatusOK)
	}
}

func (api *API) getProjectHandler() service.Handler {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
		// Get project name in URL
		vars := mux.Vars(r)
		key := vars[permProjectKey]

		withVariables := service.FormBool(r, "withVariables")
		withApplications := service.FormBool(r, "withApplications")
		withApplicationNames := service.FormBool(r, "withApplicationNames")
		withPipelines := service.FormBool(r, "withPipelines")
		withPipelineNames := service.FormBool(r, "withPipelineNames")
		withEnvironments := service.FormBool(r, "withEnvironments")
		withEnvironmentNames := service.FormBool(r, "withEnvironmentNames")
		withGroups := service.FormBool(r, "withGroups")
		withKeys := service.FormBool(r, "withKeys")
		withWorkflows := service.FormBool(r, "withWorkflows")
		withWorkflowNames := service.FormBool(r, "withWorkflowNames")
		withIntegrations := service.FormBool(r, "withIntegrations")
		withLabels := service.FormBool(r, "withLabels")

		var opts []project.LoadOptionFunc
		if withVariables {
			opts = append(opts, project.LoadOptions.WithVariables)
		}
		if withApplications {
			opts = append(opts, project.LoadOptions.WithApplications)
		}
		if withApplicationNames {
			opts = append(opts, project.LoadOptions.WithApplicationNames)
		}
		if withPipelines {
			opts = append(opts, project.LoadOptions.WithPipelines)
		}
		if withPipelineNames {
			opts = append(opts, project.LoadOptions.WithPipelineNames)
		}
		if withEnvironments {
			opts = append(opts, project.LoadOptions.WithEnvironments)
		}
		if withEnvironmentNames {
			opts = append(opts, project.LoadOptions.WithEnvironmentNames)
		}
		if withGroups {
			opts = append(opts, project.LoadOptions.WithGroups)
		}
		if withKeys {
			opts = append(opts, project.LoadOptions.WithKeys)
		}
		if withWorkflows {
			opts = append(opts, project.LoadOptions.WithWorkflows)
		}
		if withWorkflowNames {
			opts = append(opts, project.LoadOptions.WithWorkflowNames)
		}
		if withIntegrations {
			opts = append(opts, project.LoadOptions.WithIntegrations)
		}
		if withLabels {
			opts = append(opts, project.LoadOptions.WithLabels)
		}

		p, errProj := project.Load(ctx, api.mustDB(), key, opts...)
		if errProj != nil {
			return sdk.WrapError(errProj, "getProjectHandler (%s)", key)
		}

		p.URLs.APIURL = api.Config.URL.API + api.Router.GetRoute("GET", api.getProjectHandler, map[string]string{"permProjectKey": key})
		p.URLs.UIURL = api.Config.URL.UI + "/project/" + key

		if isAdmin(ctx) {
			p.Permissions.Writable = true
		} else {
			permissions, err := permission.LoadProjectMaxLevelPermission(ctx, api.mustDB(), []string{p.Key}, getUserConsumer(ctx).GetGroupIDs())
			if err != nil {
				return err
			}
			p.Permissions.Writable = permissions.Permissions(p.Key).Writable
		}

		return service.WriteJSON(w, p, http.StatusOK)
	}
}

func (api *API) putProjectLabelsHandler() service.Handler {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
		// Get project name in URL
		vars := mux.Vars(r)
		key := vars[permProjectKey]
		db := api.mustDB()

		var labels sdk.Labels
		if err := service.UnmarshalBody(r, &labels); err != nil {
			return sdk.WrapError(err, "Unmarshall error")
		}
		if err := labels.IsValid(); err != nil {
			return err
		}

		// Check if project exist
		proj, err := project.Load(ctx, db, key, project.LoadOptions.WithLabels)
		if err != nil {
			return err
		}

		var labelsToUpdate, labelsToAdd []sdk.Label
		for _, lblUpdated := range labels {
			var lblFound bool
			for _, lbl := range proj.Labels {
				if lbl.ID == lblUpdated.ID {
					lblFound = true
				}
			}
			lblUpdated.ProjectID = proj.ID
			if lblFound {
				labelsToUpdate = append(labelsToUpdate, lblUpdated)
			} else {
				labelsToAdd = append(labelsToAdd, lblUpdated)
			}
		}

		var labelsToDelete []sdk.Label
		for _, lbl := range proj.Labels {
			var lblFound bool
			for _, lblUpdated := range labels {
				if lbl.ID == lblUpdated.ID {
					lblFound = true
				}
			}
			if !lblFound {
				lbl.ProjectID = proj.ID
				labelsToDelete = append(labelsToDelete, lbl)
			}
		}

		tx, errTx := db.Begin()
		if errTx != nil {
			return sdk.WrapError(errTx, "putProjectLabelsHandler> Cannot create transaction")
		}
		defer tx.Rollback() //nolint

		for _, lblToDelete := range labelsToDelete {
			if err := project.DeleteLabel(tx, lblToDelete.ID); err != nil {
				return sdk.WrapError(err, "cannot delete label %s with id %d", lblToDelete.Name, lblToDelete.ID)
			}
		}
		for _, lblToUpdate := range labelsToUpdate {
			if err := project.UpdateLabel(tx, &lblToUpdate); err != nil {
				return sdk.WrapError(err, "cannot update label %s with id %d", lblToUpdate.Name, lblToUpdate.ID)
			}
		}
		for _, lblToAdd := range labelsToAdd {
			if err := project.InsertLabel(tx, &lblToAdd); err != nil {
				return sdk.WrapError(err, "cannot add label %s with id %d", lblToAdd.Name, lblToAdd.ID)
			}
		}

		if err := tx.Commit(); err != nil {
			return sdk.WithStack(err)
		}

		p, err := project.Load(ctx, db, key,
			project.LoadOptions.WithLabels,
			project.LoadOptions.WithWorkflowNames,
			project.LoadOptions.WithVariables,
			project.LoadOptions.WithKeys,
			project.LoadOptions.WithIntegrations,
		)
		if err != nil {
			return sdk.WrapError(err, "cannot load project updated from db")
		}

		p.Permissions.Writable = true

		event.PublishUpdateProject(ctx, p, proj, getUserConsumer(ctx))

		return service.WriteJSON(w, p, http.StatusOK)
	}
}

func (api *API) postProjectHandler() service.Handler {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
		consumer := getUserConsumer(ctx)
		if consumer == nil {
			return sdk.WithStack(sdk.ErrForbidden)
		}

		if api.Config.Project.CreationDisabled && !isAdmin(ctx) {
			return sdk.NewErrorFrom(sdk.ErrForbidden, "project creation is disabled")
		}

		var prj sdk.Project
		if err := service.UnmarshalBody(r, &prj); err != nil {
			return sdk.WrapError(err, "unable to unmarshal body")
		}

		// Check key pattern
		if rgxp := regexp.MustCompile(sdk.ProjectKeyPattern); !rgxp.MatchString(prj.Key) {
			return sdk.WrapError(sdk.ErrInvalidProjectKey, "project key %s do not respect pattern %s", prj.Key, sdk.ProjectKeyPattern)
		}

		// Check project name
		if prj.Name == "" {
			return sdk.WrapError(sdk.ErrInvalidProjectName, "project name must no be empty")
		}

		projectRunRetention := sdk.ProjectRunRetention{
			ProjectKey: prj.Key,
			Retentions: sdk.Retentions{
				DefaultRetention: sdk.RetentionRule{
					DurationInDays: api.Config.WorkflowV2.WorkflowRunRetentionDefaultDays,
					Count:          api.Config.WorkflowV2.WorkflowRunRetentionDefaultCount,
				},
			},
		}

		// Create a project within a transaction
		tx, err := api.mustDB().Begin()
		if err != nil {
			return sdk.WrapError(err, "cannot start transaction")
		}
		defer tx.Rollback() // nolint

		// Check that project does not already exists
		exist, errExist := project.Exist(tx, prj.Key)
		if errExist != nil {
			return sdk.WrapError(errExist, "cannot check if project %s exist", prj.Key)
		}
		if exist {
			return sdk.NewErrorFrom(sdk.ErrAlreadyExist, "project %s already exists", prj.Key)
		}

		if err := project.Insert(tx, &prj); err != nil {
			return err
		}

		// Check that given project groups are valid
		var groupIDs []int64
		for _, gp := range prj.ProjectGroups {
			var grp *sdk.Group
			var err error
			if gp.Group.ID != 0 {
				grp, err = group.LoadByID(ctx, tx, gp.Group.ID, group.LoadOptions.WithMembers)
			} else {
				grp, err = group.LoadByName(ctx, tx, gp.Group.Name, group.LoadOptions.WithMembers)
			}
			if err != nil {
				return err
			}

			// the default group could not be selected
			if group.IsDefaultGroupID(grp.ID) {
				return sdk.NewErrorFrom(sdk.ErrWrongRequest, "cannot use default group to create project")
			}

			// consumer should be group member to add it on a project
			if !isGroupMember(ctx, grp) {
				if isAdmin(ctx) {
					trackSudo(ctx, w)
				} else {
					return sdk.WithStack(sdk.ErrInvalidGroupMember)
				}
			}

			groupIDs = append(groupIDs, grp.ID)
		}

		// If no groups were given, try to create a new one with project name
		if len(groupIDs) == 0 {
			groupSlug := slug.Convert(prj.Name)
			existingGroop, err := group.LoadByName(ctx, tx, groupSlug)
			if err != nil && !sdk.ErrorIs(err, sdk.ErrNotFound) {
				return err
			}
			if existingGroop != nil {
				return sdk.NewErrorFrom(sdk.ErrWrongRequest, "cannot create a new group %s for given project name", groupSlug)
			}

			newGroup := sdk.Group{Name: groupSlug}
			if err := group.Create(ctx, tx, &newGroup, consumer.AuthConsumerUser.AuthentifiedUser); err != nil {
				return err
			}

			groupIDs = []int64{newGroup.ID}
		}

		// Insert all links between project and group
		for _, groupID := range groupIDs {
			if err := group.InsertLinkGroupProject(ctx, tx, &group.LinkGroupProject{
				GroupID:   groupID,
				ProjectID: prj.ID,
				Role:      sdk.PermissionReadWriteExecute,
			}); err != nil {
				return sdk.WrapError(err, "cannot add group %d in project %s", groupID, prj.Name)
			}
		}

		for _, v := range prj.Variables {
			if errVar := project.InsertVariable(tx, prj.ID, &v, consumer); errVar != nil {
				return sdk.WrapError(errVar, "addProjectHandler> Cannot add variable %s in project %s", v.Name, prj.Name)
			}
		}

		prj.Keys = []sdk.ProjectKey{
			{
				Type: sdk.KeyTypeSSH,
				Name: sdk.GenerateProjectDefaultKeyName(prj.Key, sdk.KeyTypeSSH),
			},
			{
				Type: sdk.KeyTypePGP,
				Name: sdk.GenerateProjectDefaultKeyName(prj.Key, sdk.KeyTypePGP),
			},
		}
		for i := range prj.Keys {
			k := &prj.Keys[i]
			k.ProjectID = prj.ID

			var newKey sdk.Key
			var err error
			switch k.Type {
			case sdk.KeyTypePGP:
				var email string
				email, err = api.gpgKeyEmailAddress(ctx, prj.Key, k.Name)
				if err != nil {
					return err
				}
				newKey, err = keys.GeneratePGPKeyPair(k.Name, "Project Key generated by CDS", email)
			case sdk.KeyTypeSSH:
				newKey, err = keys.GenerateSSHKey(k.Name)
			}
			if err != nil {
				return err
			}
			k.Private = newKey.Private
			k.Public = newKey.Public
			k.KeyID = newKey.KeyID
			k.LongKeyID = newKey.LongKeyID

			if err := project.InsertKey(tx, k); err != nil {
				return sdk.WrapError(err, "cannot add key %s in project %s", k.Name, prj.Name)
			}
		}

		integrationModels, err := integration.LoadModels(tx)
		if err != nil {
			return sdk.WrapError(err, "cannot load integration models")
		}

		var created, updated []sdk.ProjectIntegration
		for i := range integrationModels {
			pf := &integrationModels[i]
			created, updated, err = propagatePublicIntegrationModelOnProject(ctx, tx, api.Cache, *pf, prj, consumer)
			if err != nil {
				return sdk.WithStack(err)
			}
		}

		if err := project.InsertRunRetention(ctx, tx, &projectRunRetention); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return sdk.WithStack(err)
		}

		event.PublishAddProject(ctx, &prj, consumer)
		for _, pp := range created {
			event_v2.PublishProjectIntegrationEvent(ctx, api.Cache, sdk.EventIntegrationCreated, permProjectKey, pp, *consumer.AuthConsumerUser.AuthentifiedUser)
		}
		for _, pp := range updated {
			event_v2.PublishProjectIntegrationEvent(ctx, api.Cache, sdk.EventIntegrationUpdated, permProjectKey, pp, *consumer.AuthConsumerUser.AuthentifiedUser)
		}
		event_v2.PublishProjectEvent(ctx, api.Cache, sdk.EventProjectCreated, prj, *consumer.AuthConsumerUser.AuthentifiedUser)

		proj, err := project.Load(ctx, api.mustDB(), prj.Key,
			project.LoadOptions.WithLabels,
			project.LoadOptions.WithWorkflowNames,
			project.LoadOptions.WithKeys,
			project.LoadOptions.WithIntegrations,
			project.LoadOptions.WithVariables,
		)
		if err != nil {
			return sdk.WrapError(err, "cannot load project %s", prj.Key)
		}

		proj.Permissions.Writable = true

		return service.WriteJSON(w, *proj, http.StatusCreated)
	}
}

func (api *API) deleteProjectHandler() service.Handler {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
		// Get project name in URL
		vars := mux.Vars(r)
		key := vars[permProjectKey]

		u := getUserConsumer(ctx)
		if u == nil {
			return sdk.WithStack(sdk.ErrForbidden)
		}

		p, err := project.Load(ctx, api.mustDB(), key, project.LoadOptions.WithPipelines, project.LoadOptions.WithApplications)
		if err != nil {
			if !sdk.ErrorIs(err, sdk.ErrNoProject) {
				return sdk.WrapError(err, "deleteProject> load project '%s' from db", key)
			}
			return sdk.WrapError(err, "cannot load project %s", key)
		}

		if len(p.Pipelines) > 0 {
			return sdk.WrapError(sdk.ErrProjectHasPipeline, "project '%s' still used by %d pipelines", key, len(p.Pipelines))
		}

		if len(p.Applications) > 0 {
			return sdk.WrapError(sdk.ErrProjectHasApplication, "project '%s' still used by %d applications", key, len(p.Applications))
		}

		tx, errBegin := api.mustDB().Begin()
		if errBegin != nil {
			return sdk.WrapError(errBegin, "deleteProject> Cannot start transaction")
		}
		defer tx.Rollback() // nolint

		if err := project.Delete(tx, p.Key); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return sdk.WithStack(err)
		}

		event.PublishDeleteProject(ctx, p, getUserConsumer(ctx))
		event_v2.PublishProjectEvent(ctx, api.Cache, sdk.EventProjectDeleted, *p, *u.AuthConsumerUser.AuthentifiedUser)
		return nil
	}
}

func (api *API) getProjectAccessHandler() service.Handler {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
		vars := mux.Vars(r)

		projectKey := vars["key"]
		itemType := vars["type"]

		if !isCDN(ctx) {
			return sdk.WrapError(sdk.ErrForbidden, "only CDN can call this route")
		}

		if sdk.CDNItemType(itemType) != sdk.CDNTypeItemWorkerCache {
			return sdk.WrapError(sdk.ErrForbidden, "cdn is not enabled for this type %s", itemType)
		}

		sessionID := r.Header.Get(sdk.CDSSessionID)
		if sessionID == "" {
			return sdk.WrapError(sdk.ErrForbidden, "missing session id header")
		}

		session, err := authentication.LoadSessionByID(ctx, api.mustDBWithCtx(ctx), sessionID)
		if err != nil {
			return err
		}
		consumer, err := authentication.LoadUserConsumerByID(ctx, api.mustDB(), session.ConsumerID,
			authentication.LoadUserConsumerOptions.WithAuthentifiedUser)
		if err != nil {
			return sdk.NewErrorWithStack(err, sdk.ErrUnauthorized)
		}

		if consumer.Disabled {
			return sdk.WrapError(sdk.ErrUnauthorized, "consumer (%s) is disabled", consumer.ID)
		}

		// Add worker for consumer if exists
		worker, err := worker.LoadByConsumerID(ctx, api.mustDB(), consumer.ID)
		if err != nil && !sdk.ErrorIs(err, sdk.ErrNotFound) {
			return err
		}
		if err != nil && sdk.ErrorIs(err, sdk.ErrNotFound) {
			return sdk.WrapError(sdk.ErrForbidden, "consumer (%s) is not a worker", consumer.ID)
		}

		jobRunID := worker.JobRunID
		if jobRunID != nil {
			proj, err := project.Load(ctx, api.mustDB(), projectKey)
			if err != nil {
				return sdk.NewErrorWithStack(err, sdk.ErrUnauthorized)
			}

			nodeJobRun, err := workflow.LoadNodeJobRun(ctx, api.mustDB(), api.Cache, *jobRunID)
			if err != nil {
				return sdk.WrapError(sdk.ErrUnauthorized, "can't load node job run with id %q", *jobRunID)
			}

			if nodeJobRun.ProjectID == proj.ID {
				return service.WriteJSON(w, nil, http.StatusOK)
			}
		}

		return sdk.WrapError(sdk.ErrUnauthorized, "worker %q(%s) not authorized for project %q", worker.Name, worker.ID, projectKey)
	}
}
