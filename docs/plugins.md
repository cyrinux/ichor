# Ichor plugins

A plugin declares a Kubernetes integration (which CRDs it reads, the cards it shows, the
data it derives) in one YAML file. The Go core compiles and evaluates it; the apps only
draw the result. This page is the reference. The format's Go types are in
`go/plugin/schema.go`, and `go/plugin/schema/plugin.schema.json` is generated from them
(`cd go && go generate ./plugin`). Point an editor at it with a first line such as
`# yaml-language-server: $schema=../schema/plugin.schema.json`.

Built-in plugins live in `go/plugin/builtin/`, one file per integration. The test suite
loads, validates and compiles every one of them, renders their samples, and checks that
every label, title and message has a translation in each of the apps' languages. A built-in
plugin's detect entries also give its row in Settings, Supported integrations.

## Example: kube-green

The built-in `go/plugin/builtin/kube-green.yaml`, with its translations cut to a few lines.
Every key is described in "File format" below; the expressions in "Expressions".

```yaml
# yaml-language-server: $schema=../schema/plugin.schema.json
# kube-green puts a namespace's Deployments, StatefulSets and CronJobs to sleep on a schedule
# (SleepInfo, kube-green.com/v1alpha1) and wakes them up later. Each SleepInfo shows when it
# sleeps and wakes up next, in its own time zone, and what the controller did last (its
# status, never its state Secret's data). A sleeping namespace is never critical.
# go/ichorgo/plugin_kubegreen_test.go holds it to the reader the apps used before (the
# workload rows still use that reader: kube_kubegreen_namespace.go).
schemaVersion: ichor.dev/plugin/v1alpha1
id: kube-green
name: kube-green
website: https://kube-green.dev
icon: kube-green
category: workloads
catalogId: kube-green

detect:
  - group: kube-green.com
    resource: sleepinfos

resources:
  sleepinfo:
    group: kube-green.com
    version: v1alpha1
    resource: sleepinfos
    kind: SleepInfo
    scope: Namespaced
  # The Secret the controller keeps per SleepInfo ("sleepinfo-<name>", labelled with its
  # manager): only its name is shown, as a link; its data stays masked. Optional: an account
  # that may not list Secrets sees the schedules without the link, and no error.
  stateSecret:
    group: ""
    version: v1
    resource: secrets
    kind: Secret
    scope: Namespaced
    list: { labelSelector: "app.kubernetes.io/managed-by=kube-green" }
    optional: true

fields:
  sleepinfo:
    weekdays: $.spec.weekdays | default("")
    sleepAt: $.spec.sleepAt | default("")
    wakeUpAt: $.spec.wakeUpAt | default("")
    timeZone: $.spec.timeZone | default("UTC")
    # Absent means true for Deployments and StatefulSets, false for CronJobs (upstream defaults).
    suspendDeployments: $.spec.suspendDeployments | default(true)
    suspendStatefulSets: $.spec.suspendStatefulSets | default(true)
    suspendCronJobs: $.spec.suspendCronJobs | default(false)
    excludeCount: len($.spec.excludeRef)
    includeCount: len($.spec.includeRef)
    patchCount: len($.spec.patches)
    # What the controller did last: SLEEP, WAKE_UP, "" before its first run.
    operation: $.status.operation | default("")
    asleep: fields.operation == "SLEEP"
    lastScheduleAt: $.status.lastScheduleTime | time()
    # The schedule is "minute hour * * weekdays" per time; one part that does not parse (or an
    # unknown zone) makes it invalid: no next runs at all.
    sleepRun: cronNext(fields.sleepAt, fields.weekdays, fields.timeZone)
    wakeUpRun: 'fields.wakeUpAt == "" ? 0 : cronNext(fields.wakeUpAt, fields.weekdays, fields.timeZone)'
    validSchedule: 'fields.sleepRun != 0 && (fields.wakeUpAt == "" || fields.wakeUpRun != 0)'
    nextSleepAt: 'fields.validSchedule ? fields.sleepRun : 0'
    nextWakeUpAt: 'fields.validSchedule ? fields.wakeUpRun : 0'
    # The controller's last run expected at least 10 minutes ago (either time); missed by more
    # than 10 minutes, the controller is not running. A run due a minute ago is not late yet.
    lastSleepRun: 'fields.validSchedule ? cronPrev(fields.sleepAt, fields.weekdays, fields.timeZone, now - 600000) : 0'
    lastWakeUpRun: 'fields.validSchedule && fields.wakeUpAt != "" ? cronPrev(fields.wakeUpAt, fields.weekdays, fields.timeZone, now - 600000) : 0'
    expectedRunAt: 'fields.lastWakeUpRun > fields.lastSleepRun ? fields.lastWakeUpRun : fields.lastSleepRun'
    overdue: 'fields.expectedRunAt != 0 && fields.lastScheduleAt != 0 && fields.lastScheduleAt < fields.expectedRunAt - 600000'
    state: join(stateSecret, $.metadata.namespace == left.metadata.namespace && $.metadata.name == "sleepinfo-" + left.metadata.name)

health:
  sleepinfo:
    - when: "!fields.validSchedule"
      health: warning
      reason: invalidSchedule
      message: "invalid schedule"
    - when: fields.lastScheduleAt == 0
      health: idle
      reason: neverRan
      message: "never ran yet"
    - when: fields.overdue
      health: warning
      reason: overdue
      message: "controller missed its last run"
    - when: fields.wakeUpAt == ""
      health: warning
      reason: noWakeUp
      message: "no wake-up time"

dataServices:
  summary:
    aggregate:
      total: count(sleepinfo)
      asleep: count(sleepinfo, fields.asleep)
      warnings: count(sleepinfo, health == "warning")
    health: worst(sleepinfo.health)

cards:
  summary:
    title: { count: dataServices.summary.total, one: "%d sleep schedule", other: "%d sleep schedules" }
    health: dataServices.summary.health
    badges:
      - { label: "asleep", value: dataServices.summary.asleep, tone: info, hideWhenZero: true }
      - { label: "warnings", value: dataServices.summary.warnings, tone: warning, hideWhenZero: true }
  list:
    from: sleepinfo
    sort: [health desc, $.metadata.namespace, $.metadata.name]
    empty: "No sleep schedules."
    row:
      title: $.metadata.name
      subtitle: $.metadata.namespace
      status:
        tone: health
        text: 'fields.asleep ? "Asleep" : "Awake"'
      # Asleep: when it wakes up ("never" without a wake-up time); awake: when it sleeps.
      trailing: { text: "relativeTime(fields.asleep ? fields.nextWakeUpAt : fields.nextSleepAt)" }
      detail:
        - type: keyValues
          items:
            - { label: "Weekdays", value: fields.weekdays, mono: true }
            - { label: "Sleeps at", value: fields.sleepAt, mono: true }
            - { label: "Wakes up at", value: 'fields.wakeUpAt == "" ? "–" : fields.wakeUpAt', mono: true }
            - { label: "Time zone", value: fields.timeZone, mono: true }
            - { label: "Last run", value: fields.lastScheduleAt | relativeTime() }
        - type: badges
          items:
            - { label: "Deployments", on: fields.suspendDeployments }
            - { label: "StatefulSets", on: fields.suspendStatefulSets }
            - { label: "CronJobs", on: fields.suspendCronJobs }
        - type: badges
          items:
            - { label: "excluded", value: fields.excludeCount, tone: info, hideWhenZero: true }
            - { label: "included", value: fields.includeCount, tone: info, hideWhenZero: true }
            - { label: "patches", value: fields.patchCount, tone: info, hideWhenZero: true }
        - type: relations
          items:
            - { label: "State secret", ref: fields.state }

findings:
  - id: schedule-warning
    from: sleepinfo
    when: health == "warning"
    severity: warning
    title: "{{ namespace }}/{{ name }}"
    detail: "{{ message }}"
    opens: list

# Invented names. demo-shop sleeps on weekday nights, demo-lab sleeps on weekends with no
# wake-up time, demo-preview's schedule never ran yet. The last runs are relative to the
# clock (now-4m): a fixed date would read "controller missed its last run" a day later.
samples:
  sleepinfo:
    - metadata: { name: nightly, namespace: demo-shop }
      spec: { weekdays: "1-5", sleepAt: "20:00", wakeUpAt: "08:00", timeZone: Europe/Paris }
      status: { operation: WAKE_UP, lastScheduleTime: now-4m }
    - metadata: { name: weekend, namespace: demo-lab }
      spec: { weekdays: "6,0", sleepAt: "00:00" }
      status: { operation: SLEEP, lastScheduleTime: now-9m }
    - metadata: { name: previews, namespace: demo-preview }
      spec:
        weekdays: "*"
        sleepAt: "22:30"
        wakeUpAt: "06:00"
        suspendStatefulSets: false
        suspendCronJobs: true
        excludeRef: [{ apiVersion: apps/v1, kind: Deployment, name: gateway }]
  stateSecret:
    - metadata: { name: sleepinfo-weekend, namespace: demo-lab, labels: { app.kubernetes.io/managed-by: kube-green } }
      type: Opaque

# The apps' former kube-green strings (kubegreen_* in app/src/main/res/values-*/strings.xml).
i18n:
  fr:
    "%d sleep schedule": "%d planning de veille"
    "%d sleep schedules": "%d plannings de veille"
    "No sleep schedules.": "Aucun planning de veille."
    # … every label, in fr, de, es, it and uk
```

## File format

Other examples: `go/plugin/testdata/plugins/valid.yaml` (an older kube-green) and
`go/plugin/testdata/plugins/widgets.yaml` (every section type and aggregation).

Loading is strict:
* an unknown key is an error at its line;
* a `schemaVersion` other than `ichor.dev/plugin/v1alpha1` is refused before anything else;
* these are refused, because the apps receive everything as JSON:
  * a file over 1 MiB, or with more than 20,000 `[` and `{`;
  * nesting deeper than 64 levels, and alias bombs;
  * keys that are not text (quote `"1": …`), merge keys (`<<`);
  * empty list items, and `.nan` or `.inf`.

| Key | Required | Type | Notes |
| -- | -- | -- | -- |
| `schemaVersion` | yes | string | `ichor.dev/plugin/v1alpha1` |
| `id` | yes | string | `[a-z0-9-]+`, unique across built-in and user plugins |
| `name` | yes | string | the product name, never translated |
| `website`, `icon`, `catalogId` | no | string | `icon` and `catalogId` are app catalog ids |
| `category` | no | string | where the plugin shows on the Kubernetes home: `data-services` (default; a line of the Data services card and a tab of its screen), `networking`, `workloads` or `compute` (a tool in that family), `policies` or `security` (a tool in Security), `gitops` (a tool in Browse) |
| `screen` | no | string | `native:<route>` opens a native screen from the hub tile |
| `detect[]` | yes | list | each sets exactly one of `group` (with an optional `resource`, for a shared group), `pods` (a catalog id) or `service`; any match activates the plugin |
| `resources.<key>` | yes (≥ 1) | map | `group` (`""` for core), `version` (or `preferred`), `resource`, `kind`, `scope` (`Namespaced` or `Cluster`), `list{labelSelector, fieldSelector, limit, metadataOnly}` (`metadataOnly`: each object's metadata alone, so that a Secret's values never leave the API server), `watch`, `reveal`, `optional` (forbidden or not served, the resource reads as empty with no error: for a decorative relation), `source: native:<id>` (a Go reader selects the objects instead of a list call: `native:sealed-secrets-controller`, the Deployments running the catalog's Sealed Secrets image; `native:external-dns-controllers`, the Deployments running ExternalDNS with their flags parsed; `native:external-dns-records`, one object per hostname and address family ExternalDNS is asked to publish, matched with its controller; `native:mariadb-backups`, one object per MariaDB its Backups and PhysicalBackups name, with the latest successful and failed run (the CronJobs of the scheduled ones included) and the most frequent active schedule; `native:percona-backups`, one object per Percona XtraDB cluster its backup runs name, with the latest successful and failed run; its `group`, `version` and `resource` need not be served, they only name it; a reader that could read only part, one of its lists refused, returns what it read plus the error: the rows show, the error is an entry of `errors`, and `readable()` still holds), `ref` (with `source` only: an expression giving the object a row, a finding or a relation of this resource opens instead of the object itself, a map of `group` (`""` for core), `version`, `resource`, `kind`, `namespace` and `name`, such as `ref: $.source`; a value without `resource` or `name` opens nothing). The key (letters and digits) names the resource everywhere else |
| `fields.<resource>.<name>` | no | expression | evaluated per object in file order; a field sees the earlier ones as `fields.<name>`, `conditions`, `now`, and the resources by key for `join(<resource>, <predicate>)`, whose predicate sees `left` (the object) |
| `health.<resource>[]` | no | rules | `{when, health, reason, message}`. The first rule whose `when` holds wins, and an object no rule matches is `ok`. `health` is `ok`, `idle`, `info`, `warning`, `critical` or `unknown`; `message` is a template |
| `dataServices.<name>` | no | map | see "Data services" below |
| `cards.summary` | yes | card | `title` (an expression, or `{count, one, other}` with `%d`), `health`, `badges[]`. It sees `dataServices` and `now` only |
| `cards.<name>` | no | card | `from` (a resource or `dataServices.<name>`), `sort[]` (expressions, each optionally followed by `asc` or `desc`), `empty`, `row{title, subtitle, status{tone, text}, trailing{text}, detail[]}` |
| `actions.<id>` | no | map | `label`, `on` (a resource), `verb`, `body`, `subresource` and `method` (with `verb: subresource`), `propagationPolicy` (with `verb: delete`), `rbac{verbs, group, resource}` (required), `confirm`, `audit` (required), `enabledWhen`, `disabledReason`, `params[]`. `verb` is `mergePatch`, `jsonPatch`, `subresource`, `delete`, `annotate` or `create`; `confirm` is `none`, `plain` or `typedName`; each param is `{name, label, type, values[], default}` with `type` one of `string`, `int`, `bool` or `enum`. See "Actions" below |
| `findings[]` | no | list | `{id, from, when, severity, title, detail, opens}`. `severity` is `info`, `warning` or `critical`; `opens` names a card |
| `tools.<id>` | no | map | `label` (required), `hint`, `run: native:<id>` (required), `texts` (the English words the Go function shows in its result, translated through `i18n`): a button of the plugin screen run by Go. See "Tools" below |
| `samples.<resource>[]` | yes, for every resource | list | demo objects in the API's shape. The demo context and the tests render them; invent names such as `demo-shop`. A string `now`, `now-6h` or `now+2d` (units `s`, `m`, `h`, `d`; at most a century) becomes that time, so that the demo stays consistent with the clock |
| `i18n.<locale>` | no | map | English label → translation; a missing label stays English |

### Which names an expression sees

| Where | Names | Also |
| -- | -- | -- |
| `fields` | `$`, `fields` (earlier ones), `conditions`, `now`, `left` | resource keys (`join`) |
| `resources.<key>.ref` | `$`, `fields`, `health`, `conditions`, `now` | evaluated once per object |
| `health[].when`, `message` | `$`, `fields`, `conditions`, `now` | `when` must be a bool |
| `dataServices` `aggregate`, `health` | `fields`, `health`, `conditions`, `now`, `dataServices` (earlier ones) | resource keys; `count`, `sum`, `min`, `max`, `worst` |
| `dataServices` `groupBy`, `where`, `select` | `$`, `fields`, `health`, `conditions`, `now`, `dataServices` | |
| `cards.summary` | `dataServices`, `now` | |
| list card rows, sort, details | `$`, `fields`, `health`, `conditions`, `now`, `dataServices` | a row of a data service sees `$` (the group or projection) |
| `actions` `enabledWhen`, `disabledReason` | as a row of `on` | `enabledWhen` must be a bool |
| `actions` `body` strings (templates) | `fields`, `params`, `now` | `name`, `namespace` |
| `findings` `when` | as a row of `from` | must be a bool |
| `findings` `title`, `detail` (templates) | as a row of `from` | `name`, `namespace`, `message`, `reason` |

### Data services

A data service is one of three forms:
* `aggregate` alone, over resources: `count(res)`, `count(res, pred)`, `sum(res, expr)`, `min(res, expr)`, `max(res, expr)`, and `worst(res.health)` (or `worst(res, expr)`), with `health` for the data service's own health. It is a map of the aggregates, plus `health`. `readable(res)` is false when the resource could not be listed (forbidden, not served, failed; an `optional` one too), so that "none" is told from "none visible": `readable(key) && count(key, …) == 0`;
* `from` + `groupBy` + `aggregate`: one element per group, sorted by key, where `count()`, `count(pred)`, `sum(expr)`, `min(expr)`, `max(expr)` and `worst(expr)` run over the group's objects and `$.key` is the group's key;
* `from` + `where` + `select`: one element per kept object, with the selected names; a card row of it still opens its object.

The predicate or expression of an aggregation is evaluated once per object, with that object
as `$` and its `fields` and `health`. `count` counts the objects it holds for, `sum` adds the
numbers (an int while every one is), `min` and `max` are null without a number, and `worst`
is the worst tone (`ok` < `idle` < `info` < `unknown` < `warning` < `critical`; `ok` when
there is none). Given a value instead of a resource (`count([…])`, `worst([dataServices.a.health,
dataServices.b.health])`), they read its elements.

### Row details

Each entry of `detail[]` is a section with a `type`, and only that type's keys:

| type | Keys |
| -- | -- |
| `keyValues` | `items[{label, value, tone, mono}]` |
| `badges` | `items[{label, value or on, tone, hideWhenZero}]` |
| `gauge` | `label`, `value`, `max`, `unit`, `tone`, `text` |
| `conditions` | none: the object's `status.conditions` |
| `relations` | `items[{label, ref, text}]`, where `ref` is an object (for example a field set by `join`) |
| `table` | `from` (a resource or `dataServices.<name>`), `columns[{label, value}]` |
| `text` | `text` (a template), `tone`, `mono` |
| `actions` | `items` lists action ids, whose `on` must be the card's resource |

A `tone` is a tone word (`ok`, `idle`, `info`, `warning`, `critical`, `unknown`) or an
expression that gives one, such as `health`.

### Actions

An action is one request on one object, declared rather than coded: the app shows it as a
button in an `actions` section of a row of its `on` resource, and runs it with
`KubePluginAction(config, context, kubeServer, pluginID, actionID, namespace, name,
paramsJSON)` (namespace `""` for a cluster-scoped resource; `paramsJSON` a JSON object, `""`
for none). It returns an error only. Only an installed plugin's actions run (today the
built-in ones): a plugin file only previewed (`KubePluginPreview`, `plugin-render FILE`) gets
`allowed: false` on every button and has no way to run them.

| `verb` | Request | `body` |
| -- | -- | -- |
| `mergePatch` | `PATCH …/<resource>/<name>`, `application/merge-patch+json` | a map: the merge patch |
| `jsonPatch` | `PATCH …/<resource>/<name>`, `application/json-patch+json` | a list of `{op, path, from, value}` (RFC 6902; `op` is `add`, `remove`, `replace`, `move`, `copy` or `test`; `from` for `move` and `copy`). `op`, `path` and `from` are literal: no template, and `path` and `from` are JSON pointers |
| `annotate` | `PATCH …/<resource>/<name>`, merge patch of `metadata.annotations` | a map of annotation → string, or `null` to remove one |
| `subresource` | `PUT …/<name>/<subresource>` (`method: PUT`, the default, as virtctl does), `POST`, or `PATCH` as a merge patch (`method: PATCH`, as kubectl scale does) | a map, or none (`{}` is sent) |
| `delete` | `DELETE …/<resource>/<name>` with `DeleteOptions`: `propagationPolicy` (`Foreground`, `Background` or `Orphan`) when set, and the uid of the object just read as a precondition | none |
| `create` | `POST …/<resource>` (the collection, in the object's namespace), `application/json` | a map: the new object. Its `apiVersion`, `kind` and `metadata.namespace`, when written, must be the resource's and the object's (else the call is refused before any request); absent, `apiVersion` and `kind` are filled in |

Each call:
1. checks the plugin, the action, the namespace and the name (Kubernetes names: no `/`, no
   `..`; a cluster-scoped resource takes no namespace) and the params (at most 16 KiB of
   JSON), before any request. An action on a core Secret is refused in screenshot mode: the
   Secret would be read masked;
2. refuses in the demo context ("unavailable in the demo cluster");
3. reads the object (`GET …/<name>`, at the declared version, `preferred` resolved from
   `/apis`) and evaluates its fields and health. When `enabledWhen` or `disabledReason`
   reads a data service, or a field of the resource joins another object, the plugin's
   resources are listed in the object's namespace too;
4. refuses, with a 409 whose message is `disabledReason`, when `enabledWhen` does not hold on
   the object as the cluster has it now (one that fails to evaluate refuses too, as a 422):
   the app's disabled button is not the gate;
5. renders the body and sends the one request. The API server's refusal (403, 404, 409…) is
   the error as it said it.

With an `enabledWhen`, a patch is pinned to the object it was checked on: a merge patch
(`mergePatch`, `annotate`, `subresource` with `PATCH`) carries the object's
`metadata.resourceVersion`, a `jsonPatch` starts with a `test` of `/metadata/resourceVersion`.
An object changed in between is then refused by the API server, and the error reads
"Kind name changed since it was checked: refresh and try again". A `delete` always carries
the object's uid as a precondition.

Body templates: every string of `body` is a template that sees `$` (the object as just
read), `fields`, `params`, `now`, `name` and `namespace`. A string that is one hole and
nothing else (`"{{ params.replicas }}"`) keeps its value's type, so an `int` parameter
reaches the API as a number; any other string is text. Values under `metadata.annotations`
and `metadata.labels` (whatever their key, dots included), and every value of an `annotate`
body, are always text. Keys are never templates. A one-hole string that evaluates to null is
an error, never a null sent (which would delete a field in a merge patch): a null is only ever
a literal `null` in the YAML.

Params: `type` is `string` (at most 1,024 bytes), `int` (a JSON number with no fraction),
`bool` or `enum` (one of `values`). A param with no `default` is required; a `default` must
have the param's type (`plugin-validate` checks it). An undeclared param, a missing one or a
value of the wrong type refuses the call with an error that names the param
(`parameter "size": want an integer, got "big"`).

Security rules (what a plugin cannot do):
* The path is built only from the plugin's resource (`group`, `version`, `resource`,
  `scope`), the `namespace` and `name` given, each checked, and the YAML's `subresource`
  (lowercase letters, digits and dashes). A param or a template only ever writes the body:
  `"../../secrets"` in a param stays a string in the body.
* `verb`, `method`, `subresource` and `propagationPolicy` are fixed by the file; a body's
  shape is checked when the plugin loads (a map, a list of operations with literal `op`,
  `path` and `from`, no body for `delete`).
* `rbac` must ask for what the request needs (see "Command line"), so the can-I row answers
  for the request sent.
* The account's own rights decide: the request goes out with the cluster's credentials, and
  the API server refuses what its role does not allow.

Can-I: each action is a row of `KubeActionAccess` under the key `plugin:<id>:<action>`: one
SelfSubjectAccessReview per `rbac.verbs` entry on `rbac.group` and `rbac.resource`
(`resource/subresource` asks for the subresource), plus a `get` on the `on` resource, in the
namespace asked about for a namespaced resource. `KubePluginData` fills each button's
`allowed` from the same answers (cached a few minutes per cluster and namespace): `false`
when a review is denied, `true` when all are allowed, and absent (unknown) when a review
cannot be asked: the button stays, and the API server still decides.

Audit: every call, refused or not, is an entry of the audit log with `action`
`plugin:<id>:<action>`, the namespace, the object as `Kind/name`, the params as compact
JSON, and the outcome. `audit` in the YAML names the action for people reading the file.
Param values are written to the audit log, and to the Kubernetes Event the entry may get
(audit events on): never declare a param for a secret value. Params over 16 KiB are recorded
by size only.

Confirmation is the apps': `confirm: typedName` for what cannot be undone (`delete`, a
`stop` or `reset` subresource), `plain` (the default) for the rest, `none` for a harmless
annotation.

### Tools

A tool is a check no expression can make (resolving hostnames from the phone): `tools.<id>:
{label, hint, run: native:<id>}` puts a button on the plugin screen, and
`KubePluginTool(config, context, kubeServer, pluginID, toolID, scope, locale)` runs the Go
function `run` names over what the plugin read in `scope` (the same input as the render
tree). It returns `{title, health, cards}`, the cards as the render tree's (rows with `ref`,
`status`, `trailing`, `detail`), so the apps draw it with the plugin card renderer. The demo
context runs the tool's demo function over the `samples`, and says the tool is unavailable
when it has none. An unknown tool id, or a `run` id no Go function has, is an error when the
tool runs, not when the file is validated (as an unknown `source`). `label` and `hint` are
translated, and so are the words the Go function shows once `texts` lists them. A tool reads
only: it never changes the cluster. `native:dns-check` (ExternalDNS's "Check names") resolves
every `ExternalDNSRecord` from the phone and compares the answers with the record's targets
and its controller's TXT owner id.

### Writing expressions in YAML

An expression is a YAML scalar, so YAML reads it first:
* Quote an expression that contains `: ` (a conditional `a ? b : c`), or that starts with `[`,
  `{`, `"`, `'`, `!`, `&`, `*`, `|`, `>`, `%`, `@` or `#`. Use single quotes so the expression
  keeps its double-quoted strings: `text: 'fields.asleep ? "Asleep" : "Awake"'`.
* A string constant needs its own quotes inside the YAML ones: `title: '"Widgets"'`.
* A long expression can use a block scalar (`|`).

Problems are reported as `file:line`, and with the column when the expression sits on one
line.

## Render tree

`KubePluginData(config, context, kubeServer, pluginID, scope, locale)` reads the plugin's
resources (a namespace `scope`, `""` for all; cluster-scoped resources ignore it) and returns
one JSON tree; the apps draw it and evaluate nothing. The demo context renders the
`samples`. `KubePluginDetect(config, context, kubeServer, hints)` lists every plugin as
`{id, name, icon, category, screen, active, detectedVia, summary}`, the summary being the
tile below for an active plugin and `null` otherwise. Examples: the goldens in
`go/plugin/testdata/render/` (`kube-green-cluster.json` is read from a fake API server,
`*-samples.json` and `widgets-fr.json` from samples).

```json
{
  "plugin": {"id": "kube-green", "name": "kube-green", "icon": "kube-green", "category": "workloads", "version": "v1alpha1", "screen": "", "website": "…"},
  "active": true, "detectedVia": "api", "apiVersion": "v1alpha1",
  "health": "warning",
  "summary": {"title": "2 sleep schedules", "health": "warning", "badges": [{"label": "asleep", "value": "1", "tone": "info"}]},
  "cards": [{"id": "list", "type": "list", "empty": "No sleep schedules.", "rows": [
    {"ref": {"group": "kube-green.com", "version": "v1alpha1", "resource": "sleepinfos", "namespace": "demo-lab", "name": "weekend"},
     "title": "weekend", "subtitle": "demo-lab", "status": {"tone": "warning", "text": "Asleep"}, "trailing": {"text": "never"},
     "health": {"tone": "warning", "reason": "asleep-no-wakeup", "message": "asleep until changed by hand"},
     "detail": [{"type": "keyValues", "items": [{"label": "Sleep at", "value": "00:00"}]}]}
  ]}],
  "findings": [{"id": "asleep-no-wakeup", "severity": "warning", "title": "…", "detail": "…", "ref": {"…": "…"}, "opens": "list"}],
  "errors": [{"resource": "secrets", "message": "Kubernetes API: permission denied: forbidden"}],
  "tools": [{"id": "check", "label": "Check names", "hint": "Resolve each hostname"}]
}
```

* `tools` are the plugin's tool buttons in id order, `label` and `hint` translated; absent
  when it has none. Running one (`KubePluginTool`, see "Tools") returns
  `{"title": "…", "health": "warning", "cards": [{"id": "…", "type": "list", "rows": […]}]}`.
* A `ref` is `{group, version, resource, namespace, name}`; a resource with `ref` adds
  `kind`, the kind of the object it names, so that the apps open it without a catalog lookup.

* `detectedVia` is `api`, `pods`, `services`, `inventory` (the hints) or `demo`; `""` when
  the product is not there (`active: false`, nothing is listed, the cards are empty).
* `apiVersion` is the version read for the first detected group (`preferred` resolved).
* `health` is the summary's health: its `health` expression, else the worst object's.
* An object's health is the first matching rule's. An object of a resource without rules
  gets the object summary's reading of its conditions and phase; one that no rule matches is
  `ok`. Rows of objects carry `ref` and `health`. Without `status.tone`, a row's status tone
  is its health.
* Rows of a data service carry no `health`, and carry a `ref` only for a `select`.
* `sort` orders `health` by severity, numbers by value, the rest as text (null first).
* Booleans read `yes`/`no`, numbers keep at most two decimals, lists join with `, `.
* Section payloads:

  | type | payload |
  | -- | -- |
  | `keyValues` | `items[{label, value, tone?, mono?}]` |
  | `badges` | `items[{label, value?, tone}]`; `on` lights a badge (`ok`, or its `tone`) or dims it (`idle`); the section is left out when every badge is hidden |
  | `gauge` | `{label, value, max, unit, tone, text}`; without `text`, the two values in `unit` (`bytes`, `percent` or a word) |
  | `conditions` | `items[{type, status, reason, message, tone, lastTransition}]`; left out when the object has none |
  | `relations` | `items[{label, ref, text?}]`; an absent object is left out, and so is the section when none is left |
  | `table` | `columns[{label}]`, `rows[[cell]]` |
  | `text` | `{text, tone?, mono?}`; left out when the text is empty, so `'{{ fields.x ? "words" : "" }}'` shows only when it holds |
  | `actions` | `items[{id, label, enabled, confirm, disabledReason?, allowed?, params[]}]`; `allowed` is the can-I answer (see "Actions") |

* `i18n`: the locale's map (`pt-BR` falls back to `pt`) translates labels, `empty`, the
  summary's `one`/`other`, action labels, templates (health messages, finding texts,
  `disabledReason`), and a row's `status.text` and `text` sections (the quoted words they
  give: `"Asleep"`). Values read from objects are never
  translated.
* Reads: each resource is listed page by page with its selectors, at most `list.limit`
  objects (500 by default, up to 5000: raise it in the plugin for a kind that grows large);
  more is an `errors` entry ("showing the first N …") and sets `partial: true` on the tree
  and its `summary`, whose counts are then a lower bound and whose health is at least
  `unknown` (the apps show "N+" and say so). A
  resource that cannot be read (forbidden, not served) is an `errors` entry with the
  resource named, and the rest still renders. Secrets are masked unless `reveal`, and always
  in screenshot mode and for a plugin that is not installed (a preview,
  `plugin-render FILE`).
* A value that fails to evaluate reads empty and is reported once in `errors` with `where`
  (`fields.widget.size`).
* `just probe plugin-render FILE|ID [-scope NS] [-locale xx] -context CTX` prints the tree
  (see "Command line").

## Findings

A plugin's `findings[]` reach two places without any code of their own in the apps: the
cluster checkup and the background monitor. Both render the plugin over the whole cluster
(scope `""`) only to evaluate its findings, one per object a finding's `when` holds for; the
title and detail come out already translated (the caller's `locale`, see `i18n`), so the apps
show them as they are, notifications included. Secrets are always masked for findings.

Every finding carries the same fields in both places:

| field | value |
| -- | -- |
| `key` | `plugin:<plugin id>:<finding id>:<namespace>/<name>` (`…:<name>` for a cluster-scoped object): stable from one read to the next, the monitor's dedupe key |
| `plugin`, `pluginName`, `pluginIcon` | the plugin's `id`, `name` and `icon` |
| `id` | the finding's `id` |
| `severity` | `info`, `warning` or `critical`, the checkup's severities |
| `title`, `detail` | the finding's templates, rendered and translated (`detail` may be absent) |
| `ref` | the object, as in the render tree (`group`, `version`, `resource`, `namespace`, `name`) |
| `opens` | the card the finding links to; the apps open the plugin's screen on it |
| `user` | `true` for an imported plugin (absent for a built-in one): the apps notify on its findings only when Settings → Monitoring → "Alerts from imported plugins" is on (off by default) |

**Checkup.** `KubeCheckup(config, context, kubeServer, locale)` has a `plugins` section (its
progress step has the same id) whose findings have `kind: "plugin"`, the fields above, and
`namespace` and `name` set from `ref` like every other finding. The plugins that the cluster
does not run are skipped, and the section is `absent` when it runs none. A resource that cannot
be read (forbidden, not served) or a plugin that does not answer within 10 s names the plugin
in the section's `error` (`kube-green: sleepinfos: …`) and leaves the other plugins' findings
standing. External Secrets is left out: the `secrets` section already reports it from its own
reader. The demo shows the built-in plugins' samples.

```json
{"id": "plugins", "status": "warning", "checked": 4, "findings": [
  {"kind": "plugin", "severity": "warning", "namespace": "demo-lab", "name": "weekend",
   "key": "plugin:kube-green:schedule-warning:demo-lab/weekend",
   "plugin": "kube-green", "pluginName": "kube-green", "pluginIcon": "kube-green",
   "id": "schedule-warning", "title": "demo-lab/weekend", "detail": "no wake-up time",
   "ref": {"group": "kube-green.com", "version": "v1alpha1", "resource": "sleepinfos", "namespace": "demo-lab", "name": "weekend"},
   "opens": "list"}]}
```

**Monitor.** `KubePluginFindings(config, context, kubeServer, hints, locale)` returns every
active plugin's findings in one call, External Secrets included:

```json
{"plugins": 2,
 "findings": [{"severity": "critical", "checkupNative": true, "key": "plugin:external-secrets:target-missing:demo-lab/registry-pull",
   "plugin": "external-secrets", "pluginName": "External Secrets", "pluginIcon": "external-secrets",
   "id": "target-missing", "title": "demo-lab/registry-pull", "detail": "its Secret is missing",
   "ref": {"group": "external-secrets.io", "version": "v1", "resource": "externalsecrets", "namespace": "demo-lab", "name": "registry-pull"},
   "opens": "secrets"}],
 "errors": [{"plugin": "demo-widgets", "pluginName": "Demo Widgets", "resource": "widgets.widgets.example.com",
   "message": "Kubernetes API: permission denied: forbidden"}]}
```

* `plugins` counts the active plugins read; `findings` are worst first, then by `key`, one per
  `key`, at most 200 (`truncated` counts the rest, absent when none).
* It is bounded for a background run: one `/apis` read detects every plugin, each resource
  is read at most at its `list.limit`, and the whole call has a 10 s budget. A plugin still
  reading then is an `errors` entry (`resource` absent); its late answer is dropped.
* A resource that cannot be read is an `errors` entry with `resource`; the plugin's other
  resources, and the other plugins, still give their findings. A list cut at its limit and a
  value that fails to evaluate are not errors here (the plugin's screen reports them).
* The call fails only when the API server answers nothing; a panic in a plugin is that
  plugin's `errors` entry.
* `checkupNative: true` (absent otherwise) marks a finding of a plugin the checkup reports
  from a section of its own (External Secrets, see "Checkup" above): an app that also alerts
  on the checkup skips it, or the same object alerts twice.
* The apps notify on `warning` and `critical` (an `info` finding is shown, not notified),
  keyed by `key`: a finding seen again keeps its notification, one that is gone clears it.
* The demo context returns the plugins' samples' findings.
* `just probe plugin-findings [-locale xx] [-hints a,b] -context CTX` prints it (`-demo`: the
  samples).

## Expressions

Every computed value in a plugin (fields, health rules, data services, card values, action
bodies, findings) is an expression of a small language. It runs only in Go, cannot load
code, and every evaluation is bounded.

### Values

`null`, booleans, numbers (integers and floats), strings, lists and maps, as decoded from the
object's JSON. Integral numbers read from an object are integers.

A path that does not exist is `null`, never an error. `null` in arithmetic gives `null`, so a
missing field does not fail the whole expression.

### Names

| Name | Is |
| -- | -- |
| `$` | the object |
| `@` | the current element, inside a filter `[?( )]` |
| `fields.<name>` | a field the plugin computed for this object (earlier fields only) |
| `health` | the object's computed health (`ok`, `warning`, …) |
| `conditions.<Type>` | `true` when the object's condition `<Type>` has status `"True"`, `false` otherwise, `null` when absent |
| `left` | the outer object, inside a join predicate |
| `dataServices.<name>` | a data service's result, in cards and findings |
| `params.<name>` | an action's input, in action bodies |
| `now` | the current time in unix milliseconds |

Where an expression is written decides which names it may use. The plugin compiler reports a
name that is not available there.

### Paths

```
$.metadata.name                                  a member
$.spec.excludeRef[0].name                        a list position; [-1] is the last
$["metadata"]["labels"]["kube-green.com/x"]      a key with dots, dashes or slashes
$.spec.excludeRef[*].name                        every element: a list of names
$.status.conditions[?(@.type == "Ready")].status a filter: the statuses of the Ready conditions
```

After `[*]` or a filter, the rest of the path applies to each element, and the result is a
list (an element whose value is missing is left out). Pipe to `first()` to take one value:
`$.status.conditions[?(@.type == "Ready")].status | first()`. `[*]` over a map walks its values
in key order.

### Literals and operators

Strings take double or single quotes (`"a"`, `'a'`) with the escapes `\\ \" \' \n \r \t
\uXXXX`. Numbers are `42`, `1.5`, `2e3`; lists are `[1, "a"]`; and there are `true`, `false`,
`null`.

Operators, lowest precedence first:

| Operators | Notes |
| -- | -- |
| `\|` | pipe (below) |
| `c ? a : b` | right-associative |
| `\|\|` | short-circuit |
| `&&` | short-circuit |
| `== != < <= > >= in` | see below |
| `+ -` | `+` also joins two strings or two lists |
| `* /` | `/` is a float division; dividing by zero gives `null` |
| `! -` | unary |

* `==` and `!=` compare deeply; numbers compare by value (`3 == 3.0`). `null == null` is true;
  `null == ""` is false.
* `< <= > >=` compare two numbers or two strings. Anything else, `null` included, is false.
* `x in y` is an element of a list, a key of a map, or a substring of a string.
* `!`, `&&`, `||` and `?:` read `null`, `false`, `0`, `""`, `[]` and an empty map as false.
* `"a" + 1` is an error: convert with `string()`.

### Pipes

`x | f(args)` is `f(x, args)`. A pipe takes everything to its left, and the expression goes on
after the call:

```
$.spec.wakeUpAt | default("")              default($.spec.wakeUpAt, "")
$.a + $.b | string()                       string($.a + $.b)
$.spec.memory | quantity() > 1000000       quantity($.spec.memory) > 1000000
$.x | default(0) > 1 ? "big" : "small"     (default($.x, 0) > 1) ? "big" : "small"
$.spec.excludeRef | first().kind           first($.spec.excludeRef).kind
c ? a : b | upper()                        upper(c ? a : b)
```

Parenthesise to pipe only a part: `c ? a : (b | upper())`.

### Functions

| Function | Returns |
| -- | -- |
| `default(v, d)` | `d` when `v` is `null`; `false`, `0` and `""` are kept |
| `has(v)` | whether `v` is not `null` |
| `coalesce(a, b, …)` | the first argument that is not `null` |
| `len(v)` | the length of a string (in characters), a list or a map; 0 for `null` |
| `lower(s)`, `upper(s)` | the string in lower or upper case |
| `string(v)` | the display form: `""` for `null`, a list's elements joined by `, ` |
| `int(v)`, `float(v)` | a number from a number, a string or a bool; `null` when it does not parse |
| `join(list, sep)` | the elements joined by `sep` |
| `split(s, sep)` | the parts of `s`; `[]` for an empty string |
| `first(list)` | the first element, `null` when empty |
| `contains(x, y)` | as `y in x` |
| `matches(s, "regex")` | whether `s` matches the [RE2](https://github.com/google/re2/wiki/Syntax) pattern; write the pattern as a literal so that it compiles once |
| `quantity(q)` | a Kubernetes quantity in its base unit: `"250m"` is 0.25, `"2Gi"` is 2147483648; `null` when it does not parse |
| `bytes(v)` | a size in binary units: `"1.5 GiB"`; reads a quantity string |
| `time(s)` | an RFC 3339 timestamp in unix milliseconds; 0 when absent or invalid |
| `age(t)` | milliseconds since `t` (a timestamp or unix ms); `null` when absent |
| `duration(s)` | a Go duration string (`"1h0m0s"`, `"30m"`, as controllers write intervals) in milliseconds; `null` when it does not parse |
| `relativeTime(t)` | `"5m ago"`, `"in 3h"`, `"2d ago"`, `"now"`; `"never"` when absent |
| `cronNext(at, weekdays, tz)` | the next run in unix ms of the daily time `at` (`"20:00"`, each part cron syntax) on the cron `weekdays` (`"1-5"`, default every day) in the IANA zone `tz` (default UTC). `at` may also be a whole 5-field cron or a macro (`@daily`), then `weekdays` is ignored. 0 when it does not parse |
| `cronPrev(at, weekdays, tz[, before])` | the same schedule's last run strictly before now, or before an optional fourth argument (a time: `now - 600000`), in unix ms; 0 when it does not parse or did not run in the last 8 years |
| `condition("Type")` | the object's condition of that type (a map with `type`, `status`, `reason`, `message`…), `null` when absent; `condition(obj, "Type")` reads another object |

### Templates

Labels, messages and action bodies take text with `{{ expression }}` holes:
`"{{ $.metadata.namespace }}/{{ $.metadata.name }} sleeps"`. A value is written as by
`string()`. A `}}` inside a quoted string does not close the hole.

### Limits

* An expression, a string literal and any computed string are at most 64 KiB.
* An evaluation takes at most 10,000 steps (each node, and each element walked by `[*]` or a
  filter, and each element of a list a function or `+` builds). Parentheses, unary operators,
  conditionals and call arguments nest at most 32 deep; long `&&` chains and paths are not nesting.

Going over a limit is an error that names the expression. Parse errors carry a line and a
column. The plugin compiler also reports, without evaluating:
* unknown names and functions;
* wrong argument counts;
* a non-literal `matches` pattern;
* `"a" + 1`-style type errors;
* a condition (`when`) that is not a boolean.

## User plugins

A plugin file of your own runs on the phone like a built-in: Settings → Plugins → Import a
plugin, then paste it, pick it in Files, or give an https URL (no plain http, not even through
a redirect; 1 MiB at most). The core (`go/ichorgo/plugin_store.go`) checks it as
`plugin-validate` does and refuses, with each `plugin.yaml:LINE: message`:

* a file that does not load or validate (an unknown key, another `schemaVersion`, more than
  1 MiB, a YAML bomb);
* an `id` a built-in plugin uses: give your copy another one (`id: kube-green-copy`);
* an `id` the store cannot name a file by (more than 63 characters, or starting with `-`);
* a 65th plugin: at most 64 are kept.

Nothing is stored before the consent sheet, which lists every declared resource
(`group/resource`; a core Secret with `reveal: true`, whose values it shows in clear, in red),
every action with its verb and the Kubernetes verbs its `rbac` asks for, and whether it replaces
an imported plugin of the same `id`. Only its Enable imports the file, on; Cancel leaves the
stored plugins as they were. So every stored plugin was shown as it is stored: switching one
off and on again in the list asks nothing. Enabled, it joins the built-ins in every call (`KubePluginDetect`,
`KubePluginData`, `KubePluginAction`, the can-I rows of its actions) and reads with your own
Kubernetes access, through its declared resources only; a plugin has no network access of its
own.

The file is kept in the app's private data directory, `plugins/<id>.yaml`, encrypted with the
app's key like the scan reports, with an index of each file's sha256 and whether it is on. Each
launch checks every stored file again: one this app no longer reads (another `schemaVersion`
after an update, a built-in that now has its id, a file changed on disk) is listed off with the
reason and never loaded. Plugins are not in the app backup yet.

| Call (gomobile) | Does |
| -- | -- |
| `PluginUserCheck(yaml)` | validates the file and stores nothing; the answer is its consent entry, `replaces` when an imported plugin has its id |
| `PluginUserImport(yaml)` | the consent sheet's Enable: validates again and stores the file, on (the same id is replaced) |
| `PluginUserList()` | the imported plugins, sorted by id, with `enabled` and `error` |
| `PluginUserSetEnabled(id, enabled)` | switches one on (refused while it has an `error`) or off |
| `PluginUserRemove(id)` | deletes it and its file |
| `PluginFetchURL(url)` | downloads a file for the import screen: https only, 1 MiB, 20 s |
| `PluginCatalog()` | the installed plugins: the built-ins, then the enabled imported ones (`"user": true`) |

## Command line

The probe checks and renders plugin files from the desktop. Paths are relative to where
`just` runs. Every command but `plugin-render`, `plugin-findings` and `plugin-tool` against a cluster, and
`plugin-action`, reads no talosconfig and no kubeconfig. `plugin-render` takes a file when its argument has a `.` or a `/`
(`kube-green.yaml`, `./draft`), and a built-in plugin id otherwise (`kube-green`); a file is
validated first, before any config is read.

| Command | Does | Output | Exit |
| -- | -- | -- | -- |
| `just probe plugin-validate FILE...` | loads, validates and compiles each file, then renders its samples in English and in each `i18n` language | per valid file, one JSON line on stdout: `{"ok":true,"id":"kube-green","cards":2,"findings":1,"resources":2,"actions":0}`; per problem, `FILE:LINE: path: message` on stderr | 0 when every file is valid, 1 otherwise |
| `just probe plugin-lint FILE [-crd MANIFEST] [-strict]` | `plugin-validate`, then the warnings below | the JSON line with `"warnings":n` on stdout; `FILE:LINE: warning: path: message` on stderr, and on failure a last line `plugin-lint: N errors, M warnings` | 0 with warnings; 1 on an error, or on any warning with `-strict` |
| `just probe plugin-render FILE\|ID [-scope NS] [-locale xx] [-demo]` | the render tree of a file or of a built-in plugin id, read from the cluster (`-context CTX`) or from the samples (`-demo`) | the tree as JSON | 0, or 1 with the error (a file that does not validate: its `FILE:LINE` problems) |
| `just probe plugin-findings [-locale xx] [-hints a,b] [-demo]` | every active built-in plugin's findings, what the background monitor reads (`KubePluginFindings`), from the cluster (`-context CTX`) or the samples (`-demo`) | the report as JSON (see "Findings") | 0, or 1 when the API server answers nothing |
| `just probe plugin-action ID ACTION NAMESPACE NAME [PARAMS_JSON]` | runs a built-in plugin's action on one object (`KubePluginAction`; `""` as `NAMESPACE` for a cluster-scoped resource), `-context CTX`. It changes the cluster | `done` | 0, or 1 with the error |
| `just probe plugin-tool ID TOOL [-scope NS] [-locale xx] [-demo]` | runs a plugin's tool (`KubePluginTool`) over what it reads from the cluster (`-context CTX`) or its samples (`-demo`); read-only | the tool's result as JSON (see "Tools") | 0, or 1 with the error (`unknown tool`, a tool with no demo under `-demo`) |
| `just probe plugin-from-crd MANIFEST [-id ID] [-name NAME] [-website URL] [-category C] [-out FILE] [-force]` | writes a plugin draft for a CRD manifest (see "Writing a plugin from a CRD"), validated and linted against it; refuses a CRD a built-in plugin or a native integration already reads unless `-force` | the draft on stdout (or in `-out`); `{"ok":true,"id":"…","warnings":n}` on stderr | 0, or 1 (`already covered by built-in plugin kube-green`, a file that is not a CRD) |
| `just probe plugin-list` | the built-in plugins | one JSON line each: `{id, name, version, category, resources, cards, actions, findings, file}` | 0 |
| `just plugin-check [DIR]` | `plugin-validate` on every `*.yaml` of `DIR` (default `go/plugin/builtin`); `./build.sh check` runs it on the built-ins | as `plugin-validate` | as `plugin-validate` |

A value that fails to evaluate on a sample (`"a" + 1` reached at run time, a bad `time()`
input) is an error of `plugin-validate`, at the line of what it evaluates: the samples are the
plugin's tests.

`plugin-lint` warns (and `-strict` fails) when:
* a label, title or message has no translation in a language the plugin translates to, or the
  plugin has no `i18n` at all (one warning);
* a resource is read but nothing uses it: no card, `table`, data service or finding names it
  in `from`, and no expression (a `join` in a field, a finding or an action) names it;
* a `health` rule matches no sample (rules are first-match: a rule an earlier one always
  shadows counts as untriggered), so neither the tests nor the demo show it;
* a list card has no `empty` text;
* a resource's `ref` gives no object (a map with `resource` and `name`) on one of its
  samples: that row would open nothing;
* with `-crd MANIFEST` (a CustomResourceDefinition, YAML or JSON, as `kubectl get crd NAME -o
  yaml` prints it or as an operator's `config/crd/bases/` ships it; in a file of several
  documents or a `List`, the first CRD, other documents skipped), for the plugin resource with
  the CRD's group and plural, at its declared version (`preferred`: the storage version, else
  the last served one in list order):
  * an `additionalPrinterColumns` `jsonPath` that no expression reads (`.spec.sleepAt` is read
    by `$.spec.sleepAt`; spaces and quote styles do not matter). A column that filters on a
    condition (`.status.conditions[?(@.type=="Ready")].status`) is also read by
    `conditions.Ready` or `condition('Ready')`;
  * a condition type named in the schema (the `enum` of `status.conditions[].type`) that no
    health rule or field of the resource names (`conditions.Ready`, `condition("Ready")`);
  * a `scale` subresource and no action with `verb: subresource`, `subresource: scale`;
  * a `kind` or `scope` that differs from the CRD's, a version the CRD does not declare, or no
    resource reading the CRD at all.

An action without `rbac` is an error of `plugin-validate`, not a warning, and so is an
action whose `rbac` does not ask for what its request needs, in the resource's group:
`patch` on the resource for `mergePatch`, `jsonPatch` and `annotate`; `update`, `create` or
`patch` on `resource/subresource` for `subresource` with `PUT`, `POST` or `PATCH`; `delete`
or `create` on the resource. Otherwise the can-I row would answer for another request.

RBAC: `plugin-render` against a cluster needs `list` on the plugin's resources,
`plugin-findings` on every active plugin's (a plugin it may not read is an `errors` entry;
both read `/apis` for detection); `plugin-tool` needs what `plugin-render` does, plus what its Go function reads; `plugin-action` needs the action's `rbac` and `get` on its object;
the other commands need nothing.

## Writing a plugin from a CRD

`just probe plugin-from-crd MANIFEST` writes the draft; the `/ichor-crd-plugin` skill or a
person refines it (health rules, labels, translations), then runs `just probe plugin-validate
FILE` and `just probe plugin-lint FILE -crd MANIFEST` until both pass. The generator follows
the table below and gives the same file for the same CRD; its draft already passes both, with
the "no i18n" warning and, at most, health rules no sample triggers (listed in a comment
above `samples`). It writes one finding per severity, as a finding's `when` reads `health`,
not the rule's reason. Editors and generators can check drafts against
`go/plugin/schema/plugin.schema.json`. The exit codes are the contract; problems are
`FILE:LINE: message` lines on stderr.

Users ask for one through the Plugin request issue form:
`https://github.com/cyrinux/ichor/issues/new?template=crd-request.yml`. The `crd-request`
workflow (`.github/workflows/crd-request.yml`) fetches the CRD from the issue's link, runs
the generator and the lint, and opens a draft pull request with `go/plugin/drafts/<id>.yaml`;
a CRD the app already reads gets a comment instead. Drafts are validated but not embedded: a
maintainer reviews the health rules, translates the labels and moves the file to
`go/plugin/builtin/` (`go/plugin/drafts/README.md`).

What it reads from the CRD (`go/plugin/crd.go` decodes these):

| CRD | Plugin |
| -- | -- |
| `spec.group`, `spec.names.plural` | `detect[0]: {group, resource}` and the resource's `group`, `resource` |
| `spec.names.kind`, `spec.names.singular` | the resource's `kind`; its key (`singular`, in camel case) |
| `spec.scope` | the resource's `scope` |
| `spec.versions[]`: the one with `storage: true`, else the last served one in list order | the resource's `version` |
| `schema.openAPIV3Schema.properties.spec` | `fields`: scalars at depth ≤ 2, with `default(…)` from the schema's defaults and each `description` kept as a YAML comment |
| `properties.status` | status scalars as `fields` (`phase`, `operation`…); `status.conditions` → a `conditions` section |
| `status.conditions[].type` `enum` (`Ready`, `Available`, `Healthy`, `Synced`…) | `health` rules on `!conditions.<Type>` |
| `status.phase` `enum` | `health` rules: values with `Fail`, `Error`, `Degraded` → `critical`; `Pending`, `Progressing` → `info`; `Ready`, `Running`, `Succeeded`, `Bound` → `ok` |
| `additionalPrinterColumns[]` (`name`, `jsonPath`, `type`, `priority`) | `cards.list.row`: the name → `title`; the first other column with `priority: 0` → `status.text`; a `date` column → `trailing` with `relativeTime()`; the rest → a `keyValues` section |
| `subresources.scale` | an action `{verb: subresource, subresource: scale, rbac: {verbs: [patch], resource: <plural>/scale}, confirm: plain}` |

What it writes:
* the file `go/plugin/builtin/<id>.yaml` for a built-in (or any file for a user plugin), with
  `schemaVersion: ichor.dev/plugin/v1alpha1` and the first line
  `# yaml-language-server: $schema=../schema/plugin.schema.json`;
* `cards.summary` with a `{count, one, other}` title over a `dataServices.summary` count, and a
  `health: worst(<resource>.health)`;
* `empty` on every list card;
* `samples`: at least two objects per resource, one healthy and one failing, that together
  trigger every `health` rule, namespaced per `scope`, with invented names (`demo-shop`,
  `demo-lab`; never a real cluster's or company's) and relative times (`now-4m`);
* `i18n` for the apps' languages (`fr`, `de`, `es`, `it`, `uk`) when the plugin is built in:
  the test suite requires every label of a built-in plugin translated.
