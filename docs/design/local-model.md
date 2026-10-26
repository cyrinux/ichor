# A model on the phone instead of the OpenAI and Anthropic connector: design study

Status: study only, nothing implemented. Written against the app as of October 2026 (Go
`ai.go`, `diagnose.go`, `health_explain.go`, `prom_chat.go`; Android `AiSettings.kt`,
`DiagnosisRepository.kt`; iOS `AISettings.swift`, `TalosClient+Diagnosis.swift`) and the
on-device model landscape of the same month. Figures quoted from outside the repository are
marked as such and dated; they move fast.

## The question

Can a small model running on the phone itself, or in another app on the phone, replace the
Anthropic and OpenAI connector for the three AI features, the way a tiny image classifier
replaces a cloud vision API? And can such a model carry enough "DevOps knowledge" (Kubernetes,
Talos, etcd, PromQL) to be useful?

## Short answer

- **Partly, today, with no code**: on Android, Ollama runs inside Termux on the phone and
  the app's existing *Server URL* setting points at it (`http://127.0.0.1:11434/v1`, OpenAI
  provider, no key). This needs a test on a real phone and a paragraph in `docs/ai.md`, nothing
  else. iOS has no equivalent; there, a model on a Mac or a home server over Tailscale is the
  "local" option, and it already works.
- **Yes for the two short features, with a small amount of code**: the operating systems now
  ship a general model that any app may call: Apple's on-device foundation model (iOS 26,
  iPhone 15 Pro and later) and Google's Gemini Nano through the ML Kit Prompt API (Pixel 9/10
  and a few other flagships). Both are free, need no download and no key, and both fit the
  *Explain with AI* health helper and the PromQL panel assistant. Both have a window of about
  4 000 tokens, which the full diagnosis report often exceeds.
- **"Tiny" does not exist for this job.** An image classifier is a few million parameters
  because the task is narrow and the knowledge is in the training set's labels. Reading a
  cluster report and knowing that a `NOSPACE` alarm means `etcd defrag` and `alarm disarm`
  lives in the weights of a language model, and that knowledge only appears around 2 to 4
  billion parameters: 1.3 to 2.5 GB on disk, 3 to 4 GB of RAM while it runs, a flagship phone
  of the last two or three years. Below that size the model reads the report but does not
  know what it means. The honest tiers are in [Which model knows DevOps](#which-model-knows-devops).
- **Not recommended**: bundling weights in the APK (the release APK is 16 MB and the planned
  IzzyOnDroid listing caps apps at 30 MB), or fine-tuning a model of our own (Apple's adapter
  route was dropped with OS 27; a LoRA of our own needs a training set, an evaluation set and
  a retrain per base model, for a gain the prompt can mostly provide).

The recommended path is in [Recommendation](#recommendation): document the Termux route,
add a third provider, *This phone*, backed by the platform model where there is one, and
keep the cloud and the user's own server for the full report.

## What the connector does today

Three features share one Go client (`go/ichorgo/ai.go`): Anthropic Messages or OpenAI Chat
Completions over `net/http`, streamed, with an optional base URL so any server speaking
either protocol can be used. Each feature builds a system prompt and a user message in Go,
streams the answer to a Kotlin or Swift listener, and the Go side keeps everything that
matters for safety: the anonymization mask and its reversal, the `<report>` tag
neutralization, the PromQL verification loop, the size caps.

| Feature | What is sent | Size (measured on the sample report, `diagnose_test.go`) | Answer |
|---|---|---|---|
| Full diagnosis (`Diagnosis.Ask`) | system prompt + report + optional note | prompt 3.4 KB (about 850 tokens); sample report 1.7 KB, a real one is 5 to 30 KB with the 40-line log tails and events, capped at 256 KB | free-form, "short enough to read on a phone" |
| Health helper (`CollectHealthExplanation`) | shorter prompt + the check's lines, readiness, events; no logs, no etcd | prompt 1.9 KB (about 500 tokens); report typically 1 to 3 KB | under 180 words, fixed shape |
| Panel assistant (`PromChat`) | prompt with the source's metric names (capped, `promChatMaxMetricNames`/`promChatMaxMetricBytes`), built-in panels, the conversation | a few KB; grows with the turn history (capped at 20 turns) | one explanation and one panel block, verified against the source, up to 2 fixes |

Two things make small models more viable here than for a general chatbot:

1. **The evidence is in the prompt.** The report already says which node is not ready, which
   service failed its health check, that etcd has a `NOSPACE` alarm, that `/var` is 95 %
   full. The model is asked to read, rank and explain, not to recall the cluster. Reading
   comprehension is what 2 to 4 B models do acceptably; recall of rare facts is what they do
   badly.
2. **The output is constrained and checked.** The health helper has a fixed shape and a
   fixed list of screens; the panel assistant runs every query against Prometheus before
   showing it and sends the error back. A small model's mistakes are either caught (PromQL)
   or bounded (a 180-word answer naming screens the user then opens).

What stays hard for a small model is the full diagnosis: a long report, several unrelated
problems to rank, exact `talosctl` and `kubectl` commands with the right flags, and the
judgement about which step loses data. That is where the cloud models earn their place.

## Why a "tiny model" like the image ones does not transfer

| | Image classifier (MobileNet, ML Kit labels) | Language model that can read a cluster report |
|---|---|---|
| Parameters | 3 to 20 million | 2 to 4 billion for usable DevOps knowledge; 0.3 to 1 B models exist but only read, they do not know |
| On disk | 5 to 20 MB, ships in the app | 1.3 to 3 GB quantized, downloaded on first use |
| RAM while running | tens of MB | 2.5 to 4.5 GB plus the context |
| Latency | milliseconds | seconds to read the prompt (prefill), then 5 to 20 words per second (decode) on a 2024 flagship (vendor and blog figures, not measured by us; see sources) |
| What it knows | the 1 000 labels it was trained on | whatever was in its training text; Kubernetes is well covered, Talos thinly |
| Fits the APK | yes | no (16 MB APK today, 30 MB IzzyOnDroid limit) |

The knowledge is the weight. There is no 50 MB model that knows what `kube-scheduler` in
`CrashLoopBackOff` next to an etcd `NOSPACE` alarm means; a 50 MB model can be trained to
*classify* reports into a fixed set of causes, but the deterministic cluster checkup in the
app already does that better, with rules, and without a model.

## Which model knows DevOps

Tiers, from what the models of October 2026 do on a phone. The sizes are the usual 4-bit
quantizations.

| Tier | Examples | Disk / RAM | Phone | Health helper | Full diagnosis | PromQL |
|---|---|---|---|---|---|---|
| 0.3 to 1 B | Qwen3.5-0.8B, Gemma 3 270M/1B | 0.3 to 0.8 GB / about 1.5 GB | any 6 GB phone | follows the format, names the failing check, often wrong about the cause | no: loses the thread on a long report, invents commands | simple queries only; the verify loop catches the rest |
| 2 to 4 B | Qwen3.5-2B/4B, Gemma 3n E2B/E4B, Apple's on-device model (about 3 B), Gemini Nano (about 3 B) | 1.3 to 2.8 GB / 3 to 4.5 GB | 8 GB flagships of 2023 and later | good: reads the evidence, knows the common Kubernetes failure modes (not ready, CrashLoopBackOff, image pulls, pressure, etcd quorum) | usable on a short report when the window allows it; weak on Talos-specific commands, so the prompt must spell out the few that matter | good with the metric names and examples the prompt already gives; the fix loop makes it reliable |
| 7 to 9 B | Qwen3.5-9B, Gemma 3 12B | 5 to 7 GB / 7 to 9 GB | 12 to 16 GB phones only, hot and slow; fine on a laptop or home server | good | good: close to what the cloud models gave in 2024 | good |
| Cloud | Claude, GPT | none | any | best | best, with exact Talos commands and the data-loss warnings | best |

"Talos thinly" matters: every open model has read the Kubernetes docs and a decade of Stack
Overflow, few have read much about Talos. The system prompt already compensates (it lists what
the app can do, that there is no SSH, what needs `talosctl`), and for a small model it should
compensate more, with a short list of the Talos-specific fixes it would otherwise invent
(`talosctl etcd defrag` then `alarm disarm`, `talosctl service kubelet restart`,
`talosctl upgrade`, the reset and bootstrap warnings). Knowledge in the prompt costs tokens
but needs no training. This is the cheap substitute for fine-tuning.

No published benchmark compares small models on Kubernetes troubleshooting (searched; the
tools in that space, K8sGPT and the like, default to large hosted models). The tier table is
an expectation to verify with the evaluation set proposed under [Open points](#open-points),
not a measurement.

## The options

### A. Another app on the phone, no code: Ollama in Termux (Android)

Termux ships Ollama as a package (`pkg install ollama`, Termux from F-Droid, not the stale
Play build). `ollama serve` listens on `127.0.0.1:11434`, and its `/v1` endpoint speaks the
OpenAI Chat Completions protocol. In Ichor: provider OpenAI, *Server URL*
`http://127.0.0.1:11434/v1`, empty key (`newAIRequest` accepts plain HTTP when no key is
set), model `qwen3.5:4b` or `gemma3n:e4b`. The models list dialog works too (`AIModels` calls
`/v1/models`).

- The app's Go `net/http` is not subject to Android's cleartext policy (that applies to the
  Java stack and WebViews), so the loopback URL works without a manifest change.
- Caveats to test on a device: Android's phantom-process limit kills background child
  processes (Ollama spawns a runner per model), so users typically need the usual Termux
  workaround (`settings put global settings_enable_monitor_phantom_procs false` over adb) or
  Termux's wake lock; the first prompt pays the model load (a few seconds to tens of
  seconds); heat and battery.
- iOS: no Termux. The documented route (a Mac with Ollama or LM Studio, or a home server,
  reached over Tailscale with an `https://` or a trusted-network `http://` URL) is the local
  option there, and it already works.

Cost: a test on one or two phones, then a section in `docs/ai.md`. This should be done
whatever else is decided; it is also the way to try the models of the next option before
writing any code.

### B. The platform's own model: Apple Foundation Models and ML Kit Prompt API

Both operating systems now hand every app a general model, already on the device, with no
download, no key and no network:

| | iOS: Foundation Models framework | Android: ML Kit GenAI Prompt API (Gemini Nano) |
|---|---|---|
| Since | iOS 26 (2025); the app targets iOS 17, so a runtime check | Alpha since October 2025; a Play-services dependency (AICore), so **Play flavor only**, not `foss` |
| Devices | Apple Intelligence devices: iPhone 15 Pro and later, with Apple Intelligence enabled | Pixel 9 and 10, Galaxy Z Fold7, OnePlus 13, a list of other 2025 flagships; the list changes between page versions |
| Model | about 3 B parameters | Gemini Nano v2/v3, about 3 B |
| Window | 4 096 tokens per session, instructions and output included (TN3193); iOS 26.4 adds `contextSize` and `tokenCount(for:)` | input under 4 000 tokens; Google advises against outputs over 4 K and third-party guides against more than about 256 |
| Vendor guidance | "not intended for world-knowledge chat": summarization, extraction, classification | same positioning; an inference quota per app, foreground only |
| Custom knowledge | adapter training toolkit, final release 26.0.0, not supported from OS 27 | none |
| Streaming | yes | yes |
| License / cost | free, part of the OS | free, part of Play services |

What fits in 4 000 tokens: the health helper (about 500 tokens of prompt, 300 to 800 of
report, under 180 words out) and the panel assistant (prompt plus a few turns, with the
metric-name cap lowered for this provider). The full diagnosis fits only when the report is
short: a healthy or nearly healthy cluster, no log tails. With the token counting API (iOS
26.4) the app can tell before asking and fall back, in order, to the user's own server, the
cloud provider, or *Share with an assistant app*.

Quality: both are 3 B models tuned for instruction following and summarization, which is
exactly the health helper's shape. Expect correct identification of the failing check and
sensible "look at Nodes / Node services / Logs" steps, and occasionally a confidently wrong
cause. The fixed-shape prompt and the closed list of screens keep the damage bounded; the
"Continue in Diagnosis" button is the escape hatch.

Integration (small; the Go side already has the shape):

- A third entry in `aiProviders`, id `device`, name *This phone*, no key, no URL, no model
  field. `AIModels` returns the one model with its availability (`SystemLanguageModel.default
  .availability` on iOS, `GenerativeModel.checkStatus()` on Android), so the settings screen
  can say "not available on this phone" instead of showing a key field.
- A `LocalModel` interface in Go, implemented in Kotlin and Swift (gomobile supports
  interfaces implemented by the host language; `DiagnosisListener` already crosses the other
  way): `Generate(system, user string, sink TokenSink) error`, plus `Cancel`. `streamAnswer`
  routes to it when the provider is `device`. Everything else (mask, reveal, tag
  neutralization, size caps, PromQL verify loop, the 10-minute timeout) stays in Go, unchanged.
- The two prompts get a shorter variant for this provider: the Talos cheat-sheet lines above,
  no language list, and the metric-name cap set so the whole prompt stays under about 2 500
  tokens, leaving room for the conversation and the answer.
- Android: `com.google.mlkit:genai-prompt` in the `play` source set only; the `foss` build
  gets a stub that reports "not available" (same pattern as the Play-only install path).
- iOS: `import FoundationModels` behind `#available(iOS 26, *)`; a `LanguageModelSession`
  per request with `instructions:` as the system prompt, `streamResponse(to:)` for tokens,
  and the context-window error mapped to the fallback above.

Effort: about a week for both platforms including the settings UI, strings in six
languages, the fallbacks and the tests (a fake `LocalModel` in Go tests). Risks: the Prompt
API is alpha and its device list moves; Apple's model is tied to Apple Intelligence being
turned on, which some users keep off.

### C. An embedded engine with a downloaded model

For the `foss` flavor, phones without a platform model, and the full report, the app can
embed an inference engine and download a model on first use:

- **Engine**: llama.cpp (C++, Apache-like MIT license, Android via JNI or the
  `dev.deviceai:llm` Kotlin Multiplatform binding, iOS via its Swift package with Metal) or
  Google's LiteRT-LM / MediaPipe LLM Inference (`.litertlm` models, Android and iOS, GPU
  backends on Android). llama.cpp has the wider model choice and the GGUF ecosystem;
  LiteRT-LM has the better NPU story on Pixel and Samsung. Either adds a native library per
  ABI (a few MB with llama.cpp, more with LiteRT-LM) that must be built reproducibly like
  `ichorgo.aar` (`just verify-release`), which is the main engineering cost.
- **Model**: Qwen3.5-4B (Apache 2.0, 32 K context, about 2.5 GB at Q4) or Gemma 3n E4B
  (Gemma license, 32 K, about 2.5 GB); the 2 B variants for 8 GB phones. Downloaded from
  Hugging Face or a mirror on the user's request, checksum-verified, stored in the app's
  files; never in the APK.
- **Speed and resources** (vendor and blog figures for 2024 and 2025 flagships, to measure
  with `llama-bench` before deciding): decode 5 to 15 tokens per second for a 4 B model, 10 to
  25 for 2 B; prefill a few hundred tokens per second, so a 6 000-token report takes 15 to 40
  seconds before the first word; 3 to 4.5 GB of RAM, so the app will be killed on 6 GB phones
  with the model loaded; noticeable heat on a long answer.
- **Integration**: the same `LocalModel` interface as option B, a second implementation. The
  download manager, the storage screen (show and delete the model), the "model loaded" state
  and the memory warning are the new UI.

Effort: three to five weeks across both platforms, plus the reproducible native build and
its CI. Worth it only after option B shows that users want a phone-only answer and that the
4 K window is what they hit. Until then, Termux (option A) gives the same models to the users
who want them, at zero cost to the app.

### D. Teaching a model DevOps: fine-tuning

- Apple's adapter route (LoRA on the system model) is closed: the toolkit's last release is
  26.0.0 and it is not supported from OS 27.
- A LoRA of our own on Qwen3.5-2B/4B or Gemma 3n, distilled from a large model on synthetic
  reports: needs a corpus of reports (the demo cluster and `probe diagnose-report` can
  produce varied ones), a few thousand teacher answers, an evaluation set with graded
  answers, GPU time, hosting for the weights, and a retrain per base model. The gain is on
  Talos-specific commands and on the ranking of several problems; most of it is available
  for free by putting the Talos facts in the prompt and by shortening the report (drop the log
  tails of services that are healthy, which the report already does).

Not recommended now. The evaluation set is the one piece worth building anyway: it is what
tells whether option B or C answers well enough, and it is useful for prompt changes on the
cloud models too.

## Recommendation

1. **Now, docs only**: test Ollama in Termux on a phone and write it up in `docs/ai.md`
   under *Your own server*, with the phantom-process caveat. Note that the models list
   dialog is the quick way to see that the phone's server answers.
2. **Next release or two: provider *This phone*** (option B) for the health helper and the
   panel assistant, with the full diagnosis allowed when the report fits and a clear
   fallback when it does not. This gives iPhone 15 Pro and later and the Pixel 9 and 10 a
   free, offline, keyless answer for the two features that are used in the middle of an
   incident, which is also when the network is the thing that is broken.
3. **Evaluation set first**: twenty reports from the demo cluster and real ones (anonymized)
   with a graded expected answer, run with `probe diagnose` against the cloud models, Ollama
   on a laptop with the 2 B and 4 B candidates, and later the platform models. It decides the
   prompt variant and tells, with numbers, what the tier table above only expects.
4. **Later, if asked for**: the embedded engine (option C), llama.cpp first, for the `foss`
   build and the full report. Keep the weights out of the APK.
5. **Not**: bundled weights, fine-tuning, a 0.3 to 1 B model for the diagnosis.

## Open points

- The Termux route is reasoned from the code (plain-HTTP loopback accepted, Go HTTP outside
  the cleartext policy) and not yet tried on a phone; the phantom-process behaviour decides
  whether it is a two-line instruction or a page of caveats.
- The ML Kit Prompt API device list differs between versions of Google's own page; the
  feature must detect availability at runtime and the docs must not promise a device list.
- Whether the full diagnosis should ever go to a platform model, or always to the server or
  cloud: a short report fits, but an answer that invents a `talosctl` flag during an incident
  costs more than a "the report is too long for the phone's model" message. The evaluation
  set answers this.
- Screenshot mode and the mask: a local model gets the same masked report and the same reveal,
  so nothing changes, but the *This phone* provider should still show the report first, as the
  others do, so that the privacy promise reads the same.
- Output language: the platform models are weaker in Ukrainian than in English, French,
  German, Spanish and Italian; the prompt may have to ask for English when the app language
  is Ukrainian, and say so.

## Sources (outside the repository, October 2026)

- Apple, TN3193 "Managing the on-device foundation model's context window" (4 096 tokens;
  `contextSize` and `tokenCount(for:)` in iOS 26.4); Apple, "Get started with Foundation
  Models adapter training" (final release 26.0.0, not supported from OS 27); InfoQ on Apple's
  model report ("not intended for world-knowledge chat").
- Google, "Get started with Prompt API" (input under 4 000 tokens, output guidance, AICore
  quota, foreground only) and "Overview of the ML Kit GenAI APIs" (device lists for nano-v2
  and nano-v3); Android Developers Blog, "ML Kit GenAI Prompt API alpha release" (October 2025).
- Google, MediaPipe "LLM Inference guide" and the Gemma 3n E4B LiteRT-LM model card (32 K
  context, `.litertlm` files for Android, iOS and web).
- Termux community guides on `pkg install ollama` (prebuilt package, Play build stale, 8 GB
  of RAM advised); Ollama docs (`/v1` OpenAI-compatible endpoint, loopback by default).
- Qwen3.5 small series announcements and model cards (0.8/2/4/9 B, Apache 2.0, 4 B at Q4
  about 2.5 GB); llama.cpp mobile bindings (`dev.deviceai:llm`, Liquid AI's llama.cpp mobile
  notes with the `llama-bench` recipe).
- IzzyOnDroid FAQ and maintainer posts (30 MB per-app limit, per-ABI builds as the usual fix).
