package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Labels and annotations a cluster owner sets on a CronJob to tune how Ichor shows it. Each
// is read from the annotations first, then the labels (a label value cannot hold spaces).
const (
	// cronIconKey names the icon: a bundled app icon or a Dashboard Icons slug ("postgresql").
	cronIconKey = "ichor.levis.name/icon"
	// cronTitleKey is a display name ("Database backup") instead of the CronJob's name.
	cronTitleKey = "ichor.levis.name/title"
	// cronDescriptionKey is one line on what the job does.
	cronDescriptionKey = "ichor.levis.name/description"
	// cronTriggerKey set to "false" hides the manual run (a job that must only run on schedule).
	cronTriggerKey = "ichor.levis.name/trigger"
)

// cronInstantiateAnnotation is what `kubectl create job --from=cronjob/NAME` sets on the Job.
const cronInstantiateAnnotation = "cronjob.kubernetes.io/instantiate"

// cronRunsKept is how many recent Jobs are shown per CronJob.
const cronRunsKept = 5

// Run states, of one Job and of a CronJob (its latest run).
const (
	cronRunRunning   = "running"
	cronRunSucceeded = "succeeded"
	cronRunFailed    = "failed"
	cronRunNever     = "never" // no run yet, or none kept
)

type kubeCronJob struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	// Icon names the bundled icon, RemoteIcon a Dashboard Icons slug (see inventoryApp).
	// Neither: the app draws its default CronJob icon.
	Icon         string       `json:"icon,omitempty"`
	RemoteIcon   string       `json:"remoteIcon,omitempty"`
	Schedule     string       `json:"schedule"`
	TimeZone     string       `json:"timeZone,omitempty"`
	Suspended    bool         `json:"suspended"`
	Triggerable  bool         `json:"triggerable"`
	Active       int          `json:"active"`       // Jobs running now
	State        string       `json:"state"`        // the latest run's, see cronRun*
	LastSchedule int64        `json:"lastSchedule"` // unix ms, 0 when never
	LastSuccess  int64        `json:"lastSuccess"`  // unix ms, 0 when never
	NextRun      int64        `json:"nextRun"`      // unix ms, 0 when suspended or unknown
	Created      int64        `json:"created"`      // unix ms
	Images       []string     `json:"images"`
	Runs         []kubeJobRun `json:"runs"` // newest first, at most cronRunsKept
}

type kubeJobRun struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Manual   bool   `json:"manual"`   // started by hand (Ichor, kubectl create job --from)
	Started  int64  `json:"started"`  // unix ms
	Finished int64  `json:"finished"` // unix ms, 0 while running
}

type kubeCronJobList struct {
	CronJobs []kubeCronJob `json:"cronJobs"`
}

type kubeObjectMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	Labels            map[string]string `json:"labels"`
	Annotations       map[string]string `json:"annotations"`
	CreationTimestamp time.Time         `json:"creationTimestamp"`
	OwnerReferences   []struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"ownerReferences"`
}

// meta is an Ichor setting of the object: its annotation, else its label.
func (m kubeObjectMeta) meta(key string) string {
	if v := strings.TrimSpace(m.Annotations[key]); v != "" {
		return v
	}

	return strings.TrimSpace(m.Labels[key])
}

type cronJobObject struct {
	Metadata kubeObjectMeta `json:"metadata"`
	Spec     struct {
		Schedule    string  `json:"schedule"`
		TimeZone    *string `json:"timeZone"`
		Suspend     *bool   `json:"suspend"`
		JobTemplate struct {
			Metadata struct {
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Spec json.RawMessage `json:"spec"`
		} `json:"jobTemplate"`
	} `json:"spec"`
	Status struct {
		Active             []struct{} `json:"active"`
		LastScheduleTime   *time.Time `json:"lastScheduleTime"`
		LastSuccessfulTime *time.Time `json:"lastSuccessfulTime"`
	} `json:"status"`
}

// images are the container images of the Job template.
func (c cronJobObject) images() []string {
	var spec struct {
		Template struct {
			Spec struct {
				Containers []struct {
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	}

	images := []string{}
	if json.Unmarshal(c.Spec.JobTemplate.Spec, &spec) == nil {
		for _, ct := range spec.Template.Spec.Containers {
			images = append(images, ct.Image)
		}
	}

	return images
}

func (c cronJobObject) triggerable() bool {
	v := strings.ToLower(c.Metadata.meta(cronTriggerKey))

	return v != "false" && v != "disabled" && v != "no" && v != "0"
}

type jobObject struct {
	Metadata kubeObjectMeta `json:"metadata"`
	Status   struct {
		StartTime      *time.Time `json:"startTime"`
		CompletionTime *time.Time `json:"completionTime"`
		Conditions     []struct {
			Type               string    `json:"type"`
			Status             string    `json:"status"`
			LastTransitionTime time.Time `json:"lastTransitionTime"`
		} `json:"conditions"`
	} `json:"status"`
}

// cronOwner is the CronJob that created the Job, "" for a Job of its own.
func (j jobObject) cronOwner() string {
	for _, o := range j.Metadata.OwnerReferences {
		if o.Kind == "CronJob" {
			return o.Name
		}
	}

	return ""
}

func mapJobRun(j jobObject) kubeJobRun {
	run := kubeJobRun{
		Name:   j.Metadata.Name,
		State:  cronRunRunning,
		Manual: j.Metadata.Annotations[cronInstantiateAnnotation] == "manual",
	}

	if j.Status.StartTime != nil {
		run.Started = j.Status.StartTime.UnixMilli()
	} else if !j.Metadata.CreationTimestamp.IsZero() {
		run.Started = j.Metadata.CreationTimestamp.UnixMilli()
	}

	for _, c := range j.Status.Conditions {
		if c.Status != "True" {
			continue
		}

		switch c.Type {
		case "Complete":
			run.State = cronRunSucceeded
		case "Failed":
			run.State = cronRunFailed
		default:
			continue
		}

		if !c.LastTransitionTime.IsZero() {
			run.Finished = c.LastTransitionTime.UnixMilli()
		}
	}

	if j.Status.CompletionTime != nil {
		run.Finished = j.Status.CompletionTime.UnixMilli()
	}

	return run
}

// KubeCronJobs lists the CronJobs of every namespace with their recent runs, through the
// Kubernetes API with the admin kubeconfig Talos issues (os:admin):
// {"cronJobs":[{namespace,name,title,description,icon,remoteIcon,schedule,timeZone,
// suspended,triggerable,active,state,lastSchedule,lastSuccess,nextRun,created,images,
// runs:[{name,state,manual,started,finished}]}]}. kubeServer: see KubePods.
func KubeCronJobs(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubeCronJobList { return kubeCronJobList{CronJobs: demoCronJobs(time.Now())} },
		func(ctx context.Context, k *kubeClient) (kubeCronJobList, error) {
			return listCronJobs(ctx, k, time.Now())
		})
}

func listCronJobs(ctx context.Context, k *kubeClient, now time.Time) (kubeCronJobList, error) {
	var (
		crons            []cronJobObject
		runs             map[string][]kubeJobRun
		cronErr, jobsErr error
		wg               sync.WaitGroup
	)

	wg.Go(func() {
		cronErr = k.listAll(ctx, "/apis/batch/v1/cronjobs", pageQuery{}, func() { crons = nil }, func(page kubePage) error {
			objs, err := decodeItems[cronJobObject](page)
			crons = append(crons, objs...)

			return err
		})
	})
	wg.Go(func() { runs, jobsErr = listCronRuns(ctx, k, "", func(string) bool { return true }) })
	wg.Wait()

	if err := errors.Join(cronErr, jobsErr); err != nil {
		return kubeCronJobList{}, err
	}

	out := make([]kubeCronJob, 0, len(crons))
	for _, c := range crons {
		out = append(out, mapCronJob(c, runs[c.Metadata.Namespace+"/"+c.Metadata.Name], now))
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}

		return out[i].Name < out[j].Name
	})

	return kubeCronJobList{CronJobs: out}, nil
}

func mapCronJob(c cronJobObject, runs []kubeJobRun, now time.Time) kubeCronJob {
	m := c.Metadata
	cj := kubeCronJob{
		Namespace:   m.Namespace,
		Name:        m.Name,
		Title:       m.meta(cronTitleKey),
		Description: m.meta(cronDescriptionKey),
		Schedule:    c.Spec.Schedule,
		Suspended:   c.Spec.Suspend != nil && *c.Spec.Suspend,
		Triggerable: c.triggerable(),
		Active:      len(c.Status.Active),
		Images:      c.images(),
	}

	cj.Icon, cj.RemoteIcon = cronJobIcon(m.meta(cronIconKey), m.Namespace, m.Name, cj.Images)

	if c.Spec.TimeZone != nil {
		cj.TimeZone = *c.Spec.TimeZone
	}

	if !m.CreationTimestamp.IsZero() {
		cj.Created = m.CreationTimestamp.UnixMilli()
	}

	if t := c.Status.LastScheduleTime; t != nil {
		cj.LastSchedule = t.UnixMilli()
	}

	if t := c.Status.LastSuccessfulTime; t != nil {
		cj.LastSuccess = t.UnixMilli()
	}

	if !cj.Suspended {
		cj.NextRun = cronNextRun(cj.Schedule, cj.TimeZone, now)
	}

	sort.Slice(runs, func(i, j int) bool { return runs[i].Started > runs[j].Started })

	if len(runs) > cronRunsKept {
		runs = runs[:cronRunsKept]
	}

	cj.Runs = append([]kubeJobRun{}, runs...)
	cj.State = cronJobState(cj)

	return cj
}

// cronJobState is the state of the latest run; without any Job left, what the status says.
func cronJobState(cj kubeCronJob) string {
	switch {
	case cj.Active > 0:
		return cronRunRunning
	case len(cj.Runs) > 0:
		return cj.Runs[0].State
	case cj.LastSuccess > 0 && cj.LastSuccess >= cj.LastSchedule:
		return cronRunSucceeded
	default:
		return cronRunNever
	}
}

// cronNextRun is the next scheduled run in unix ms, 0 when the schedule or its time zone
// cannot be read. Without a time zone, the controller's own: UTC on Talos.
func cronNextRun(schedule, timeZone string, now time.Time) int64 {
	sched, err := parseCronSchedule(schedule)
	if err != nil {
		return 0
	}

	loc := time.UTC
	if timeZone != "" {
		if loc, err = time.LoadLocation(timeZone); err != nil {
			return 0
		}
	}

	next := sched.next(now.In(loc))
	if next.IsZero() {
		return 0
	}

	return next.UnixMilli()
}

// iconSlugPattern is what the apps accept as an icon name (see isValidIconSlug in IchorCore):
// safe as a file name and in a URL path.
var iconSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,80}$`)

// cronJobIcon resolves the icon label: a bundled app icon, else a Dashboard Icons slug. With
// no label, the app its images (else its name or namespace) belong to, as on the Apps screen.
// A value that is not a slug is ignored. Both empty: the default CronJob icon.
func cronJobIcon(label, namespace, name string, images []string) (icon, remote string) {
	catalog := loadAppCatalog()

	if label != "" {
		slug := strings.ToLower(strings.TrimSpace(label))
		if !iconSlugPattern.MatchString(slug) {
			return "", ""
		}

		if app := catalog.byName[slug]; app != nil && app.hasIcon() {
			return app.ID, ""
		}

		return "", slug
	}

	var id identification

	for _, image := range images {
		if id = catalog.identify(parseImageRef(image)); isMain(id) {
			break
		}
	}

	if !isMain(id) && name != "" {
		id = catalog.byPod(namespace, name)
	}

	switch {
	case id.app != nil && id.app.hasIcon():
		return id.app.ID, ""
	case id.slug != "":
		return "", id.slug
	default:
		return "", ""
	}
}

// KubeTriggerCronJob runs a CronJob now, like `kubectl create job --from=cronjob/NAME`
// (os:admin): a Job from its template, owned by the CronJob so its history and cleanup
// apply. A suspended CronJob can be run; one labelled ichor.levis.name/trigger=false is
// refused. Returns the new Job's name. kubeServer: see KubePods.
func KubeTriggerCronJob(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := validateKubeName("CronJob", namespace, name); err != nil {
		return "", err
	}

	err = kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		job, err := triggerCronJob(ctx, k, namespace, name)
		out = job

		return err
	})

	return out, err
}

var errCronJobTriggerDisabled = &kubeAPIError{Code: 403, Reason: "Disabled", Message: "manual runs of this CronJob are disabled (" + cronTriggerKey + "=false)"}

func triggerCronJob(ctx context.Context, k *kubeClient, namespace, name string) (string, error) {
	ns := url.PathEscape(namespace)

	var c cronJobObject
	if err := k.get(ctx, "/apis/batch/v1/namespaces/"+ns+"/cronjobs/"+url.PathEscape(name), &c); err != nil {
		return "", err
	}

	if !c.triggerable() {
		return "", errCronJobTriggerDisabled
	}

	annotations := map[string]string{}
	for key, v := range c.Spec.JobTemplate.Metadata.Annotations {
		annotations[key] = v
	}

	annotations[cronInstantiateAnnotation] = "manual"

	spec := c.Spec.JobTemplate.Spec
	if len(spec) == 0 {
		spec = json.RawMessage("{}")
	}

	job := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"generateName": manualJobPrefix(name),
			"namespace":    namespace,
			"labels":       c.Spec.JobTemplate.Metadata.Labels,
			"annotations":  annotations,
			"ownerReferences": []map[string]any{{
				"apiVersion": "batch/v1", "kind": "CronJob", "name": name, "uid": c.Metadata.UID, "controller": true,
			}},
		},
		"spec": spec,
	}

	var created struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	if err := k.post(ctx, "/apis/batch/v1/namespaces/"+ns+"/jobs", job, &created); err != nil {
		return "", err
	}

	return created.Metadata.Name, nil
}

// manualJobPrefix is the generateName of a manual run: NAME-manual-, short enough for the
// 5 random characters the API server adds to stay within a label value (63).
func manualJobPrefix(name string) string {
	const suffix = "-manual-"

	if limit := 63 - 5 - len(suffix); len(name) > limit {
		name = strings.TrimRight(name[:limit], "-.")
	}

	return name + suffix
}
