# AI diagnosis (optional)

Part of the [Ichor documentation](../README.md#documentation).


Off by default. Turn it on in Settings → AI diagnosis; until then the app shows no trace of it
and never contacts a model provider.

- **What it does:** it reads the cluster state into a report, shows you that report, and only
  when you tap *Ask* sends it to the model you chose. The answer names the likely cause, the
  evidence and the steps to fix it, and is written as it arrives.
- **The report:** node readiness and stage, Talos services, memory, disks, clock offset, etcd,
  control-plane static pods, pods without a running container, recent warning and error events,
  the last 40 log lines of services that are not healthy and, with an os:admin talosconfig,
  the storage, database and backup operators that are not fine (Longhorn, Garage,
  CloudNativePG, Velero, cert-manager…), with the nodes involved, and the workloads not ready and
  the pods in trouble. It never contains the talosconfig, a machine configuration or a kubeconfig.
- **On a kubeconfig cluster the report covers** what the Kubernetes API knows, read with your own
  credentials within 20 seconds and cut at 32 KiB: the nodes with their pressure conditions, the
  workloads with fewer ready pods than they want (a workload scaled to zero is not listed) and why,
  the pods in CrashLoopBackOff, ImagePullBackOff or Pending (30 at most), the newest 30 Warning
  events, the checkup's warnings and criticals, the plugins' critical findings, failed Helm
  releases, and the Argo CD and Flux apps that are not fine. It needs to list nodes, pods, events,
  Deployments, StatefulSets and DaemonSets across the cluster: what your credentials may not list
  is left out, with a line saying so.
- **Anonymized by default:** the nodes' names, IP addresses and domain, the Kubernetes
  namespaces and the names of pods, containers, GitOps apps and data service objects are
  replaced with placeholders (`cp-1`, `10.0.0.7`, `homelab.lan`, `ns-1`, `pod-3`) before the
  report leaves the phone, and the real ones are put back in the answer on the phone.
  `kube-system`, `default`, `kube-public`, `flux-system` and `argocd` stay, as do the static pods
  Kubernetes names itself (`kube-apiserver-`, `kube-controller-manager-`, `kube-scheduler-`, the
  node masked) and the container names of pods in those namespaces. The mask only knows the
  names the report's data states and those the kubelet logs as `pod="…"`: the rest of the log
  lines (other host names such as a registry or an API endpoint, a name in free text) is sent as
  it is, so read the report first if your logs are sensitive. The Kubernetes sections (the whole
  report of a kubeconfig cluster) go further: images, and in their free text whatever is quoted
  or shaped like a name (`acme-compute`, `registry.example.net/app:1`), become `img-1` and `obj-2`.
  A single plain word that nothing marks as a name can still pass there.
- **Secrets stripped, in every mode:** what looks like one (`Authorization` and `Bearer` values,
  `token=`, `password=`, `apikey=`, AWS and GitHub keys, JWTs, PEM blocks, long hex or base64
  runs other than image digests and container ids) becomes `[redacted]`, even when the names go
  as they are, and the report's last line counts them.
- **Providers:** Anthropic (Claude, default `claude-opus-5-5`) or OpenAI (default
  `gpt-6-astra`), with your own API key; any model can be typed or picked from the provider's
  list. The key is stored encrypted (Android Keystore, iOS Keychain) and is only sent over
  HTTPS, to the provider or to the server URL you set for it.
- **Your own server:** set *Server URL* to a gateway or a local model speaking the Anthropic
  Messages API (`https://host`) or the OpenAI Chat Completions API (`http://host:11434/v1`).
  Plain HTTP is refused unless the server needs no key and is on your own network: a loopback,
  link-local or private address (RFC 1918, IPv6 ULA) or a name ending in `.local`, `.internal` or
  `.lan`. The report then crosses that network in clear.
- **This phone:** the third provider runs a model on the phone itself, with
  [llama.cpp](https://github.com/ggml-org/llama.cpp): no key, no server, and nothing leaves the
  phone. Pick a model in Settings → AI diagnosis → This phone and tap *Download*; the size is
  shown on the button (Qwen3.5 4B, about 3.0 GB, for phones with 8 GB of memory or more on
  Android and 12 GB on iPhone; Qwen3.5 2B, about 1.4 GB, for smaller ones). Or *Import a model
  file* to use a GGUF file you already have, without the phone contacting Hugging Face at all.
  - **The download:** starts only when you tap. It runs on Wi-Fi, or another network that is
    not metered, unless you turn on *Allow mobile data for this download* (off again for the
    next one). It never runs while roaming, nor on mobile data while Android's Data Saver or
    iOS's Low Data Mode is on: it pauses with the reason shown, and goes on where it stopped
    once the network allows it. The file comes from Hugging Face at a revision pinned in the
    app, and its size and SHA-256 are checked before it is used; a file that does not match is
    deleted. Models are kept out of backups, and *Delete the model* frees the space. On
    Android the download needs Android 14 or later, where it shows in the system's list of
    active apps with its progress and Cancel in a notification; older versions offer *Import a
    model file* only.
  - **The answers:** a 2 to 4 billion parameter model knows Kubernetes but little of Talos, and
    is often wrong. It gets shorter instructions with the Talos fixes spelled out, and reads
    8 192 tokens in all: the health check helper and the panel assistant fit, the full report of
    a cluster with many problems may not, and the app then says so (ask that one with another
    provider, or share it). The first question loads the model (a few seconds); it is freed a
    minute after the last answer, or when the app goes to the background.
  - **Builds:** the engine is part of every build. `ICHOR_LOCAL_MODEL=0 ./build.sh` leaves it out
    of the Android app, which then does not offer this provider.
- **Without an API key:** *Share with an assistant app* hands the same question and report to
  the Claude, ChatGPT or Gemini app (or any app taking text) through the system share sheet.
- **Check the answer:** a model can be wrong. Read a command before running it, especially one
  that resets a node or changes etcd membership.
- **Health check helper:** when the cluster health check fails, the health page shows *Explain
  with AI*: one tap sends the check's lines and failure, node readiness and recent warning and
  error events (no logs, no etcd), anonymized like the report, and the answer names the likely
  cause and the next two or three screens to check. *Continue in Diagnosis* opens the full
  report with the failure as the note.
- **Panel assistant:** on the Metrics screen, the sparkle button (or *Ask AI* in the panel
  editor) opens a chat with the same model to write a PromQL panel from a description, or to
  change the one being edited. The model gets the metric names your source knows and the
  built-in panels as examples; every query it proposes is run against your source before it is
  shown, and a query the source turns down goes back to the model, which gets two more tries.
  *Use this panel* fills the editor. What is sent: your messages, the source's metric names and
  the panel being edited, as they are (not anonymized); never the cluster's names or addresses.

From a workstation, the same code runs against a real cluster:

```sh
cd go
go run ./cmd/probe diagnose-report        # the anonymized report; nothing is sent
# These two send the report with the real names (not anonymized) and print the answer:
ANTHROPIC_API_KEY=... go run ./cmd/probe diagnose anthropic
OPENAI_API_KEY=... go run ./cmd/probe diagnose openai gpt-6-astra
# The health check helper: runs the health check, then prints what would be sent (anonymized,
# nothing is sent), or sends it with the real names and prints the answer
go run ./cmd/probe health-explain-report
ANTHROPIC_API_KEY=... go run ./cmd/probe health-explain anthropic
# The panel assistant: the metric names (nothing is sent), then one question with its checked panel
SRC='{"mode":"proxy","namespace":"monitoring","service":"prometheus-operated","port":9090}'
go run ./cmd/probe prom-metrics "$SRC"
ANTHROPIC_API_KEY=... go run ./cmd/probe prom-chat "$SRC" anthropic "memory by namespace, top ten"
```

## Evaluating a model or a prompt change

`go/ichorgo/testdata/ai_eval/` holds invented reports (an etcd NOSPACE alarm, a crash loop, a
healthy cluster, a report that tries to give the model orders…), each with the words a good answer
must hold and the ones it must not. `just ai-eval anthropic <model>` sends them to the model with
your API key, prints one row per case and a total, and fails when the mean score is under 0.8.
`just probe diag-eval openai <model> etcd-nospace` runs one case; `ICHOR_AI_BASE_URL` points it at
any OpenAI-compatible server. A case's score is the share of its required words found, 0 if it holds
a forbidden one or runs past its word limit: a rough number to compare two runs, not a judgement of
the answer. Only the set's reports are sent, nothing from a cluster. The phone's model runs only in
the apps, so `device` says so and skips.
