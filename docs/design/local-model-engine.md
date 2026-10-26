# A model on the phone, option C: embedded engine with a downloaded model (plan)

Status: plan, nothing implemented. Follows the study in [local-model.md](local-model.md)
(option C). Written against the app as of October 2026: Go `ai.go`, `diagnose.go`,
`prom_chat.go`; Android `AiSettings.kt`, `AiSection.kt`, `UpdateManager.kt`,
`LocalNetwork.kt`, the monitor's WorkManager use; iOS `AISettings.swift`, `AISection.swift`,
`MeteredNetwork.swift`, `AppSupport.swift`; `build.sh`, `scripts/verify-release.py`,
`flake.nix`. Figures about llama.cpp and the models are from outside the repository and
dated; the spike in [Phase 0](#phase-0-spike-one-week-gonogo) measures them.

## Goals

1. A fourth way to get an answer, next to Anthropic, OpenAI and the user's own server: a
   model file on the phone, run by an engine inside the app, for the three AI features
   (diagnosis, health helper, panel assistant). Works with no network, no key, no account.
2. **It stays an option**, in three senses, each a hard rule:
   - **For the user**: off by default like the rest of the AI diagnosis. No model is
     downloaded, loaded or run unless the user picks *This phone* and taps *Download*. The
     model can be deleted from the same screen. With the provider not selected, the app
     behaves byte for byte as today: no file, no memory, no network.
   - **For the build**: the engine is a small native library, built from pinned source in
     `build.sh` like the Go core, and a Gradle property (`ichor.localModel=false`) or an iOS
     build setting drops it entirely (no library, no code path, no settings row). The
     default build ships it: the library is a few MB per ABI, the APK stays well under
     IzzyOnDroid's 30 MB.
   - **For the phone**: the provider appears only where it can work (64-bit ARM, enough
     memory) and says why not elsewhere, instead of failing after a 2.8 GB download.
3. **The download respects the user's data rules**: Wi-Fi only by default, nothing on a
   metered network unless the user allows mobile data for this download, nothing ever on
   roaming, nothing when Android's Data Saver or iOS Low Data Mode is on, pause when the
   network stops qualifying, resume where it stopped. The size is shown before the tap.
4. Everything that protects the user today stays in Go, unchanged: the report shown first,
   the anonymization mask and its reversal, tag neutralization, size caps, the PromQL
   verification loop, the timeout.

## Non-goals

- Weights in the APK or the IPA. Never: 1.5 to 2.8 GB per model.
- A model of our own (fine-tuning): see the study, option D.
- Replacing the cloud providers: the full diagnosis on a long report stays better there.
- 32-bit phones (`armeabi-v7a`): no engine library; the provider says "needs a 64-bit
  phone". They do not have the memory anyway.
- Running while the app is in the background, or a chat that keeps the model loaded all day.

## Engine

**llama.cpp**, pinned to one commit, built from source. Reasons over LiteRT-LM / MediaPipe:
MIT license and a plain C API; GGUF, the format every open model ships in, so the catalog
can change models without changing the engine; one code base for Android (CPU) and iOS
(Metal); a `build-xcframework.sh` and an Android CMake path maintained upstream; small
library. LiteRT-LM has the better NPU story on Pixel and Samsung, which is worth a look later
if the CPU path proves too slow; it is a larger dependency with a Google-controlled model
format.

- **Android**: CMake with the NDK already pinned in `flake.nix` (r29), `GGML_NATIVE=OFF`,
  `BUILD_SHARED_LIBS=ON`, `GGML_OPENMP=OFF`, examples, tools, tests and server off, linked
  with `-Wl,-z,max-page-size=16384` like the Go core. ABIs: `arm64-v8a` and `x86_64` (the
  emulator); none for `armeabi-v7a`. CPU features: baseline `armv8.2-a+dotprod`, with
  llama.cpp's runtime CPU-variant dispatch (`GGML_CPU_ALL_VARIANTS` with `GGML_BACKEND_DL`)
  for `i8mm` and SVE where the spike shows it builds for Android; otherwise two libraries
  and a `System.loadLibrary` choice from `/proc/cpuinfo` features. Expected size 2 to 4 MB
  compressed per ABI, to be measured.
- **iOS**: `build-xcframework.sh` from the pinned commit (static, Metal with the embedded
  metallib, iOS 16.4 minimum, below the app's iOS 17), added to `ios/project.yml` next to
  `Ichorgo.xcframework`. The release's prebuilt `llama.xcframework`, pinned by checksum, is
  the fallback if building it in `scripts/ios-build.sh` is too slow for CI.
- **Binding**: a thin JNI file of our own (about 300 lines of C++: load, tokenize, count
  tokens, apply the GGUF chat template, decode in a loop, cancel flag, unload), rather than
  `examples/llama.android` which is a sample and moves. Kotlin side: `LlamaEngine` in
  `app/src/main/java/name/levis/ichor/localmodel/`. Swift side: the C API through the
  xcframework's module, `LlamaEngine.swift` in `ios/Ichor/Services/`.
- **Reproducibility**: the source is a tarball of the pinned commit with its SHA-256 in
  `build.sh` (next to the gomobile pins), built in the Nix shell with `-ffile-prefix-map`
  and no timestamps. `just verify-release` already rebuilds the whole APK from a clean
  checkout and compares it, so the new library is covered without a new tool; the first
  release with it needs one diffoscope pass to catch build-path or timestamp leaks.
- **CI**: the library is built once per run and cached under `.cache/llama/` keyed on the
  pinned commit and the NDK version, like the AAR.

## Model catalog

A short, static list in Go (`go/ichorgo/local_models.go`, exported through
`AIModels("device", "", "")` so both apps show the same entries), one entry per model:

| Field | Example |
|---|---|
| `id` | `qwen3.5-4b-q4` |
| `name` | Qwen3.5 4B (recommended) |
| `url` | the Hugging Face `resolve/<commit>/` URL of the GGUF, pinned by commit hash so the bytes never change under the same URL |
| `bytes`, `sha256` | shown before the download; checked after |
| `license` | Apache 2.0, with the link shown in the picker |
| `minMemoryBytes` | 8 GiB for the 4B, 6 GiB for the 2B |
| `context` | 8 192 tokens, the context the app opens |
| `prompt` | `small`: the prompt variant to use |

Initial entries: **Qwen3.5-4B** Q4_K_M (about 2.8 GB, Apache 2.0) as the recommended one,
**Qwen3.5-2B** Q4_K_M (about 1.5 GB) for 6 GB phones and iPhones with 8 GB (see
[Memory](#memory)). Gemma 3n is left out at first: its terms of use carry a pass-through
obligation for distributors that the app would have to show and the user accept; Apache 2.0
has none. To be confirmed by reading the current Gemma terms before a later addition.

The catalog ships with the app, so a new model or a corrected checksum comes with an app
release (and goes through `verify-release`). A later step can move it to the GitHub release
assets, the way the updater reads releases, if the list changes more often than the app.

**Bring your own file**: *Import a model file* in the same screen takes any GGUF through the
system document picker (copied into the app's files, no checksum to check, the user's
choice). It is the route for people who do not want the phone to talk to Hugging Face at
all, and for air-gapped use; it is also what makes the feature independent of any download
server.

## Download

### Rules, both platforms

1. **Explicit**: only a tap on *Download* starts it; the button shows the size ("Download,
   2.8 GB"). The app never restarts a download on its own after a deletion or a failure; a
   paused one resumes on its own only while the user's rules allow it.
2. **Wi-Fi only by default**: the download runs only on an unmetered network. A phone
   hotspot and a Wi-Fi the system marks as metered count as mobile data. A switch *Allow
   mobile data for this download*, off by default and not remembered across downloads,
   turns that off; turning it on shows the size again and asks to confirm.
3. **Data Saver and Low Data Mode win**: with Android's Data Saver on, or iOS Low Data
   Mode on the current network, no download on a metered network, even with the switch on.
   The screen says "Data Saver is on" or "Low Data Mode is on" instead of a spinner. (On
   Android, an app the user exempted from Data Saver is allowed but still shown the warning:
   Google's guidance is to economize on metered networks whatever the exemption.)
4. **Never on roaming.**
5. **Pause and resume**: when the network stops qualifying mid-download (Wi-Fi drops, the
   phone switches to mobile data), the download pauses and shows "Waiting for Wi-Fi"; it
   resumes from the same byte when a qualifying network is back (HTTP `Range` with the
   ETag checked; a changed ETag restarts from zero). Cancel deletes the partial file.
6. **Space first**: before starting, the free space must exceed the size plus 10 %; the
   partial file is written as `<id>.gguf.part` and renamed once the checksum matches.
7. **Integrity**: SHA-256 streamed while writing, compared with the catalog at the end; a
   mismatch deletes the file and says so.
8. **Not backed up, not transferred**: the model directory is under the app's files, which
   `data_extraction_rules.xml` and `AppSupport.swift` already exclude.
9. **Battery**: not started under 15 % unless charging; a running download is not stopped
   for the battery (it finishes in minutes on Wi-Fi), the system's own throttling applies.

### Android

A WorkManager worker (`ModelDownloadWorker`), since WorkManager is already a dependency and
the monitor uses it:

- Constraints: `NetworkType.UNMETERED` by default, `CONNECTED` when the user allowed
  mobile data; `setRequiresStorageNotLow(true)`, `setRequiresBatteryNotLow(true)`.
  WorkManager then holds the work while the constraint is not met and restarts it when it
  is, which gives rule 5 for free; the worker resumes from the `.part` file's length.
- Inside the worker, before the first byte and on every network change (a
  `ConnectivityManager.NetworkCallback` registered for the download's lifetime):
  `isActiveNetworkMetered` (`LocalNetwork.kt` has the helper), `getRestrictBackgroundStatus()`
  for Data Saver (`RESTRICT_BACKGROUND_STATUS_ENABLED` blocks on a metered network;
  `WHITELISTED` allows with the warning), `NET_CAPABILITY_NOT_ROAMING` for roaming.
  When a rule fails, the worker returns `retry()` with the reason stored for the UI, and
  WorkManager waits for the next constraint change.
- Long-running: `setForeground` with a progress notification (pause and cancel actions),
  `foregroundServiceType="dataSync"` on WorkManager's foreground service and the
  `FOREGROUND_SERVICE_DATA_SYNC` permission (the app's two services use `specialUse`; a
  download is what `dataSync` is for and Play accepts it without a declaration). Android
  14's 6-hour limit per day for `dataSync` is far above a download that takes minutes.
- The HTTP loop is the updater's (`UpdateManager.download`: `HttpURLConnection`, 64 KB
  buffer, progress to a `StateFlow`), with `Range`, `If-Range`, the streamed digest and the
  byte cap set to the catalog's size plus a margin. Redirects from Hugging Face to its CDN
  are followed for the file only; the updater's no-redirect rule stays for the APK.
- The system `DownloadManager` was considered (`setAllowedOverMetered(false)` and
  `setAllowedOverRoaming(false)` give rules 2 and 4 by themselves): rejected because it
  cannot stream the checksum, pauses on its own rules rather than ours, writes outside the
  app's private files unless pointed at external storage, and some users disable it.

### iOS

A background `URLSession` (`URLSessionConfiguration.background(withIdentifier:)`), so the
download survives the app being suspended:

- `allowsExpensiveNetworkAccess = false` (cellular and hotspots) unless the user allowed
  mobile data; `allowsConstrainedNetworkAccess = false` always (Low Data Mode is the user's
  data saver; no switch overrides it); `allowsCellularAccess` follows the switch;
  `isDiscretionary = false` (the user asked for it now); `waitsForConnectivity` is implicit
  for background sessions; `sessionSendsLaunchEvents = true` with
  `handleEventsForBackgroundURLSession` in the app delegate to finish the file move after
  a relaunch.
- A network that stops qualifying fails the task with `networkUnavailableReason` of
  `.expensive` or `.constrained`; the app keeps the `resumeData`, shows "Waiting for Wi-Fi"
  or "Low Data Mode is on", and resumes with `downloadTask(withResumeData:)` when
  `NWPathMonitor` (`MeteredNetwork.swift`) reports a qualifying path. Roaming has no flag:
  the app reads `CTCarrier` is gone, so it relies on `isExpensive`, which is true on cellular.
- Free space from `volumeAvailableCapacityForImportantUsage`; the file lands in Application
  Support through `AppSupport.swift` (already excluded from backup); SHA-256 computed after
  the move with `CryptoKit` in a stream.
- Progress in the settings row; a Live Activity like the CA rotation's is optional polish.

## Runtime

### Go side

- A new provider in `aiProviders`: `{ID: "device", Name: "This phone"}`, no key URL. In
  `newAIRequest`, `device` needs neither key nor base URL; `AIModels("device", …)` returns
  the catalog with each entry's state (`downloaded`, `bytes`, `ready`).
- A Go interface implemented by Kotlin and Swift, registered once at start
  (`SetLocalModel`), the same direction as `DiagnosisListener` but inverted:

  ```go
  type LocalModel interface {
      // Status is JSON {available, reason, modelId, downloaded, loaded, memoryOK}.
      Status() string
      // CountTokens tells whether the prompt fits the model's context; -1 when not loaded.
      CountTokens(text string) int
      // Generate streams the answer for the conversation (system apart) to sink; it loads
      // the model on first use. It returns an error string: "" on success, "cancelled",
      // "not-downloaded", "too-long", "out-of-memory", or a message.
      Generate(modelID, system, messagesJSON string, sink LocalTokenSink) string
      Cancel()
      Unload()
  }
  ```

  `streamAnswer` routes `device` to it; `aiError` maps its errors to the existing
  messages ("the report is too large for this model" already exists for HTTP 413, "the
  model is not downloaded: Settings → AI diagnosis" is new).
- **Prompt variant**: `diagnosisSystemPromptSmall` and `healthExplainSystemPromptSmall` in
  the prompt files, chosen when the provider is `device`: the same rules, shorter, plus the
  Talos cheat sheet from the study (the few `talosctl` commands a small model otherwise
  invents), an explicit "answer in English" when the app language is Ukrainian (the small
  models are weak there; the UI says so once). The panel assistant lowers
  `promChatMaxMetricNames` for this provider so the prompt stays under about 2 500 tokens.
- **Fit check**: before `Generate`, the Go side asks `CountTokens` for the whole prompt;
  over the model's context minus 1 024 for the answer, it returns the "too large" error at
  once, and the UI offers the three other routes (server, cloud, share sheet). No partial
  prompt, no truncated report: a diagnosis on half a report is worse than none.
- Tests: a fake `LocalModel` in Go tests (like `fakeProvider`), covering routing, the fit
  check, cancel, the error mapping and that the mask is applied and reversed as for HTTP.

### Engine lifecycle

- Load on the first *Ask*, in a background thread with a "Loading the model" state (3 to
  15 s from flash); keep loaded while a diagnosis, health or metrics screen is open and
  for 60 s after; unload on `onTrimMemory` / `didReceiveMemoryWarning`, when the app goes to
  the background, when the provider changes and when the model is deleted.
- Context 8 192 tokens, batch 512, threads = performance cores (big cores from
  `/proc/cpuinfo` on Android, `activeProcessorCount` on iOS), temperature 0.3, the GGUF's
  own chat template with thinking disabled for Qwen (template variable
  `enable_thinking=false`), stop at end of turn. Metal on iOS, CPU on Android at first
  (OpenCL on Adreno and Vulkan exist in llama.cpp, device-specific; a later option).
- Cancel: the user's *Stop* sets the flag the decode loop checks every token; `Unload`
  frees the context and the model.
- Heat and time: the first token on a long report can take 15 to 40 s on a 2023 flagship;
  the UI shows "Reading the report (1 200 tokens)" with the count, and after 60 s without
  a token offers to stop.

### Memory

- Android: `ActivityManager.MemoryInfo.totalMem` against the catalog's `minMemoryBytes`;
  under it the model is listed greyed with "needs a phone with 8 GB of memory". The 4B
  model with its 8 K context uses about 3.3 GB resident; Android kills the app above what
  the device allows a foreground app, so the 6 GB phones only get the 2B.
- iOS: `ProcessInfo.physicalMemory`; iOS limits an app to roughly half of the device's
  memory (jetsam), so an 8 GB iPhone runs the 2B model and the 4B only on 12 GB devices
  (iPhone 17 Pro class). The catalog carries a separate `minMemoryBytesIOS`. The
  increased-memory-limit entitlement is iPad only.

## Settings and screens

- **Settings → AI diagnosis → provider**: a third segment, *This phone*. Selected, the
  card shows: the model picker (catalog, each with size and license; *Import a model
  file* at the end), the state line (*Not downloaded* / *Waiting for Wi-Fi* / *Data Saver
  is on* / *Downloading 43 %, 1.2 of 2.8 GB* / *Ready, 2.8 GB on the phone*), the buttons
  (*Download 2.8 GB* / *Pause* / *Cancel* / *Delete*), the switch *Allow mobile data for
  this download* (off), and one line: "Downloads start only when you tap, on Wi-Fi unless
  allowed, never with Data Saver on. The model answers on the phone; nothing is sent."
  The key, model id and server URL fields are hidden for this provider. The anonymize
  switch stays (the report is still shown first, and screenshot mode still applies).
- Where the engine is absent (32-bit, build without it) or the phone too small, the segment
  is shown with the reason in the card and no download button, so the user learns why
  instead of looking for the feature.
- **Diagnosis, health helper, panel assistant**: unchanged, except the states above
  (*Loading the model*, the token count while reading, the "too large" fallback with the
  three other routes). The *Ask* button is disabled with "Download the model first" and a
  link to the settings when nothing is downloaded.
- Strings in the six languages, `just i18n-check`.
- **docs/ai.md**: a *This phone* paragraph: what is downloaded from where (Hugging Face
  sees the phone's address; the import route avoids it), the data rules, the memory needs,
  what to expect from a 4B model against the cloud ones, and that the report is shown
  first like everywhere else.

## Phases

### Phase 0: spike (one week, go/no-go)

Build `libllama.so` for `arm64-v8a` and the iOS xcframework from a pinned commit in the
Nix shell; a throwaway activity and a throwaway view that load Qwen3.5-2B and 4B and run the
study's prompts on the sample report and on a 20 KB report with log tails; measure on three
phones (a 2023 8 GB Android flagship, a 6 GB mid-range, an 8 GB iPhone): library size per
ABI, load time, first-token time, tokens per second, resident memory, whether the phone
throttles; run the study's evaluation set and grade. Go if: library under 5 MB compressed
per ABI, the 4B gives a first token under 30 s on a 6 000-token report on the flagship, no
kill on the 6 GB phone with the 2B, and the graded answers are usable on the health helper
and the panel assistant. Also settled here: CPU-variant dispatch on Android, the Gemma
terms question, Hugging Face `Range` support on its CDN.

### Phase 1: Android (two weeks)

The engine build in `build.sh` with the Gradle property, the JNI binding, `LlamaEngine`,
the Go provider, interface, catalog, prompt variants and tests, the download worker with
all the rules above, the settings card, the screen states, strings, `docs/ai.md`, one
`verify-release` pass with diffoscope on the new library.

### Phase 2: iOS (one and a half weeks)

The xcframework in `scripts/ios-build.sh` and `project.yml`, `LlamaEngine.swift`, the
background session download with the rules, the settings card and states, the memory
table for iPhones.

### Phase 3: polish (one week, as needed)

Import a model file; the download notification's actions and the Live Activity; OpenCL or
Vulkan on Android if the spike showed the CPU too slow; moving the catalog to release
assets; a *Storage* line showing the model's size with the app's other data.

## Open points

- CPU-variant dispatch on Android: if `GGML_CPU_ALL_VARIANTS` does not build for the NDK,
  the choice is two libraries (dotprod, dotprod+i8mm) or the baseline only; the spike tells
  by how much i8mm matters on the 4B.
- iOS memory ceiling: whether the 4B runs at all on 8 GB iPhones without a jetsam kill,
  with Metal's buffers counted; the catalog's iOS minimum follows the measurement.
- Hugging Face as the only download source: a mirror on the project's GitHub releases
  (release assets allow 2 GB files, which fits the 2B but not the 4B at Q4_K_M) or a
  split download; or ship the catalog with two URLs per model.
- The GGUF parser runs in-process on a file from the network: the pinned checksums keep
  the catalog's files honest, the import route is the user's own risk, and the pinned
  llama.cpp commit should be moved forward on its security fixes like any dependency.
- Whether Data Saver's `WHITELISTED` state should block like `ENABLED`: the plan allows
  with a warning, following Google's guidance; a stricter reading blocks both.
- Play listing: the data-safety form stays unchanged (the download is user-initiated, no
  data collected), but the store description should say the model is a separate 2.8 GB
  download.
