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

## File format

A complete example is `go/plugin/testdata/plugins/valid.yaml` (kube-green).

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
| `category` | no | string | `data-services` (default), `networking`, `gitops`, `policies`, `workloads`, `compute`, `security` |
| `screen` | no | string | `native:<route>` opens a native screen from the hub tile |
| `detect[]` | yes | list | each sets exactly one of `group` (with an optional `resource`, for a shared group), `pods` (a catalog id) or `service`; any match activates the plugin |
| `resources.<key>` | yes (≥ 1) | map | `group` (`""` for core), `version` (or `preferred`), `resource`, `kind`, `scope` (`Namespaced` or `Cluster`), `list{labelSelector, fieldSelector, limit}`, `watch`, `reveal`, `optional` (forbidden or not served, the resource reads as empty with no error: for a decorative relation), `source: native:<id>`. The key (letters and digits) names the resource everywhere else |
| `fields.<resource>.<name>` | no | expression | evaluated per object in file order; a field sees the earlier ones as `fields.<name>`, `conditions`, `now`, and the resources by key for `join(<resource>, <predicate>)`, whose predicate sees `left` (the object) |
| `health.<resource>[]` | no | rules | `{when, health, reason, message}`. The first rule whose `when` holds wins, and an object no rule matches is `ok`. `health` is `ok`, `idle`, `info`, `warning`, `critical` or `unknown`; `message` is a template |
| `dataServices.<name>` | no | map | see "Data services" below |
| `cards.summary` | yes | card | `title` (an expression, or `{count, one, other}` with `%d`), `health`, `badges[]`. It sees `dataServices` and `now` only |
| `cards.<name>` | no | card | `from` (a resource or `dataServices.<name>`), `sort[]` (expressions, each optionally followed by `asc` or `desc`), `empty`, `row{title, subtitle, status{tone, text}, trailing{text}, detail[]}` |
| `actions.<id>` | no | map | `label`, `on` (a resource), `verb`, `body`, `subresource` (with `verb: subresource`), `rbac{verbs, group, resource}` (required), `confirm`, `audit` (required), `enabledWhen`, `disabledReason`, `params[]`. `verb` is `mergePatch`, `jsonPatch`, `subresource`, `delete`, `annotate` or `create`; `confirm` is `none`, `plain` or `typedName`; each param is `{name, label, type, values[], default}` with `type` one of `string`, `int`, `bool` or `enum` |
| `findings[]` | no | list | `{id, from, when, severity, title, detail, opens}`. `severity` is `info`, `warning` or `critical`; `opens` names a card |
| `samples.<resource>[]` | yes, for every resource | list | demo objects in the API's shape. The demo context and the tests render them; invent names such as `demo-shop`. A string `now`, `now-6h` or `now+2d` (units `s`, `m`, `h`, `d`; at most a century) becomes that time, so that the demo stays consistent with the clock |
| `i18n.<locale>` | no | map | English label → translation; a missing label stays English |

### Which names an expression sees

| Where | Names | Also |
| -- | -- | -- |
| `fields` | `$`, `fields` (earlier ones), `conditions`, `now`, `left` | resource keys (`join`) |
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
* `aggregate` alone, over resources: `count(res)`, `count(res, pred)`, `sum(res, expr)`, `min(res, expr)`, `max(res, expr)`, and `worst(res.health)` (or `worst(res, expr)`), with `health` for the data service's own health. It is a map of the aggregates, plus `health`;
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
  "plugin": {"id": "kube-green", "name": "kube-green", "icon": "kube-green", "category": "data-services", "version": "v1alpha1", "screen": "", "website": "…"},
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
  "errors": [{"resource": "secrets", "message": "Kubernetes API: permission denied: forbidden"}]
}
```

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
  | `badges` | `items[{label, value?, tone}]`; `on` lights a badge (`ok`, or its `tone`) or dims it (`idle`) |
  | `gauge` | `{label, value, max, unit, tone, text}`; without `text`, the two values in `unit` (`bytes`, `percent` or a word) |
  | `conditions` | `items[{type, status, reason, message, tone, lastTransition}]`; left out when the object has none |
  | `relations` | `items[{label, ref, text?}]`; an absent object is left out, and so is the section when none is left |
  | `table` | `columns[{label}]`, `rows[[cell]]` |
  | `text` | `{text, tone?, mono?}` |
  | `actions` | `items[{id, label, enabled, confirm, disabledReason?, params[]}]`; `allowed` comes with the actions step |

* `i18n`: the locale's map (`pt-BR` falls back to `pt`) translates labels, `empty`, the
  summary's `one`/`other`, action labels, templates (health messages, finding texts,
  `disabledReason`) and a row's `status.text`. Values read from objects are never
  translated.
* Reads: each resource is listed page by page with its selectors, at most `list.limit`
  objects (500 by default, up to 5000: raise it in the plugin for a kind that grows large);
  more is an `errors` entry ("showing the first N …") and sets `partial: true` on the tree
  and its `summary`, whose counts are then a lower bound and whose health is at least
  `unknown` (the apps show "N+" and say so). A
  resource that cannot be read (forbidden, not served) is an `errors` entry with the
  resource named, and the rest still renders. Secrets are masked unless `reveal`.
* A value that fails to evaluate reads empty and is reported once in `errors` with `where`
  (`fields.widget.size`).
* `just probe plugin-render ID [-scope NS] [-locale xx] -context CTX` prints the tree.

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
