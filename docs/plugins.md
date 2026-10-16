# Ichor plugins

A plugin declares a Kubernetes integration (which CRDs it reads, the cards it shows, the
data it derives) in one YAML file. The Go core compiles and evaluates it; the apps only
draw the result. This page is the reference; the file format itself is documented with the
plugin loader.

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
