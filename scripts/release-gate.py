#!/usr/bin/env python3
"""The daily auto-release's CI gate: may the commit be tagged, or should it wait or re-run?

Reads on stdin, as JSON, each required workflow's latest push run on the commit, as
`gh run list --workflow <file> --commit <sha> --event push --limit 1 --json
databaseId,conclusion,status,attempt,workflowName` prints it, keyed by workflow file:

    {"android.yml": [{...run...}], "ios.yml": []}

and prints one JSON decision:

    {"decision": "release" | "rerun" | "wait" | "blocked", "rerun": [run ids],
     "blocked": [run ids], "reason": "..."}

- every workflow's run succeeded -> release
- a run failed (or was cancelled, or timed out) on its first attempt -> rerun that run
- a run failed on its second attempt or later -> blocked: it is not retried again
- a run still queued or in progress (a re-run included), or no run yet -> wait

blocked wins over rerun, and rerun over wait: a commit that cannot pass is reported at
once, and a failed first attempt is re-run without waiting for the other workflow.
"""

import json
import sys

FIRST_ATTEMPT = 1


def classify(workflow, runs):
    """(state, run id or None, why) for one workflow's latest run."""
    if not runs:
        return "wait", None, f"{workflow}: no run yet"
    run = runs[0]
    run_id = run.get("databaseId")
    if run.get("status") != "completed":
        return "wait", run_id, f"{workflow}: {run.get('status') or 'pending'}"
    conclusion = run.get("conclusion") or "unknown"
    if conclusion == "success":
        return "success", run_id, f"{workflow}: success"
    attempt = int(run.get("attempt") or FIRST_ATTEMPT)
    if attempt <= FIRST_ATTEMPT:
        return "rerun", run_id, f"{workflow}: {conclusion} on attempt {attempt}"
    return "blocked", run_id, f"{workflow}: {conclusion} on attempt {attempt}"


def decide(runs_by_workflow):
    states = [classify(workflow, runs) for workflow, runs in sorted(runs_by_workflow.items())]
    reason = "; ".join(why for _, _, why in states)
    ids = {kind: [run_id for state, run_id, _ in states if state == kind] for kind in ("rerun", "blocked")}
    kinds = {state for state, _, _ in states}
    if not states:
        decision = "wait"
        reason = "no workflow to check"
    elif "blocked" in kinds:
        decision = "blocked"
    elif "rerun" in kinds:
        decision = "rerun"
    elif "wait" in kinds:
        decision = "wait"
    else:
        decision = "release"
    return {"decision": decision, "rerun": ids["rerun"] if decision == "rerun" else [],
            "blocked": ids["blocked"], "reason": reason}


def main():
    try:
        runs_by_workflow = json.load(sys.stdin)
    except json.JSONDecodeError as err:
        print(f"release-gate: stdin is not JSON: {err}", file=sys.stderr)
        return 2
    if not isinstance(runs_by_workflow, dict):
        print("release-gate: expected an object of workflow file -> runs", file=sys.stderr)
        return 2
    print(json.dumps(decide(runs_by_workflow)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
