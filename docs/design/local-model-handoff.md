# A model on the phone: hand-off

Status on 2026-10-26, for whoever picks up PR #685 (branch `ccr-d1b95128-wr84po`). The study
is [local-model.md](local-model.md) and the plan [local-model-engine.md](local-model-engine.md).

## Where it stands

- **PR #685 is green and mergeable**: Android check, debug, release and Play jobs, and the iOS
  core tests, simulator build and unsigned IPA. No reviews yet.
- **Implemented**: the "This phone" AI provider on Android and iOS, the llama.cpp engine, the
  download with its network rules, model import, settings and screens, strings in six
  languages, docs, privacy policy and store notes. The details are in the PR description.
- **Not usable for downloads yet**: the model catalog is not pinned (next section). Until it
  is, the app offers only *Import a model file*.
- **Never run on a phone**: no model has been loaded or asked a question yet (Phase 0 below).

## Next steps, in order

1. **Pin the models.** From any machine that reaches huggingface.co, on this branch:

   ```sh
   python3 scripts/local-models.py          # fills revision, bytes, sha256
   python3 scripts/local-models.py --check  # fails if an entry is still unpinned
   cd go && go test ./ichorgo -run LocalModel
   ```

   Commit `go/ichorgo/local_models.json`. The script fails when a file is not in its
   repository: the two file names (`bartowski/Qwen_Qwen3.5-4B-GGUF`,
   `Qwen_Qwen3.5-4B-Q4_K_M.gguf`, and the 2B one) were written from search results and never
   checked against Hugging Face. Fix the repo or file name in the JSON if it fails. The cloud
   sessions that wrote this PR could not reach huggingface.co (proxy 403, also from a fresh
   session after the domain was allowed, possibly in another environment or on a container
   started before the change).
2. **Phase 0 on real phones** (plan, "Phase 0"): install the debug APK on an 8 GB Android
   flagship, a 6 GB phone and an iPhone; download or import Qwen3.5 2B and 4B; ask the health
   helper, the panel assistant and the full diagnosis on the demo cluster and a real one.
   Measure load time, time to first word, words per second, memory, heat. Check that the
   answer starts without a reasoning block (`assistantPrefix`) and that the Talos commands are
   right.
3. **Adjust from Phase 0**: the memory minimums in `local_models.json` (`minMemoryMiB`,
   `minMemoryMiBIOS` are estimates), the context size (8 192), the thread count, and the
   small prompts in `go/ichorgo/diagnose_prompt.go` and `health_explain_prompt.go`.
4. **First release with the engine**: run `just verify-release <tag>` and diffoscope once on
   `lib/*/libichorllm.so` (build flags aim at reproducibility, not yet proven); see
   CONTRIBUTING.md "Reproducible builds".

## Measured in CI

| Release APK, unsigned | main | with the engine |
|---|---|---|
| arm64-v8a | 33.4 MB | 35.2 MB |
| x86_64 | 39.7 MB | 41.7 MB |
| armeabi-v7a (no engine) | 34.2 MB | 34.3 MB |

`libichorllm.so` is 4.4 MB (arm64) and 4.9 MB (x86_64) uncompressed. It must be stripped at
link time (`-Wl,--strip-all` in `app/src/main/cpp/CMakeLists.txt`): Gradle cannot strip
native libraries in this build ("Unable to strip the following libraries"), and unstripped
it added 12.8 MB.

## Where things are

| What | Where |
|---|---|
| Go provider, engine interface, think filter, error mapping | `go/ichorgo/ai_device.go` |
| Model catalog and its loader | `go/ichorgo/local_models.json`, `local_models.go` |
| Small-model prompts | `go/ichorgo/diagnose_prompt.go`, `health_explain_prompt.go`, `prom_chat_prompt.go` |
| Engine (C API over llama.cpp, shared) | `native/llm/ichor_llm.{h,cpp}` |
| llama.cpp pin (b11550) and fetch | `native/llama-cpp.env`, `scripts/fetch-llama.sh` |
| Android native build | `build.sh` (`build_llm_jni`), `app/src/main/cpp/` |
| Android app code | `app/src/main/java/name/levis/ichor/localmodel/`, `ui/settings/LocalModelSection.kt`, `ui/diagnosis/AiTarget.kt` |
| iOS app code | `ios/Ichor/Services/LocalModelHost.swift`, `LocalModels.swift`, `ios/Ichor/Views/LocalModelSection.swift` |
| iOS engine build | `scripts/ios-build.sh` (llama.xcframework), `ios/project.yml` |
| Model pinning | `scripts/local-models.py` (tests: `scripts/test_local_models.py`) |

## Decisions to know

- **Optional at three levels**: off by default for the user; `ICHOR_LOCAL_MODEL=0 ./build.sh`
  drops the engine from Android (the provider is then hidden); the phone itself is checked
  (64-bit, dot-product CPU, memory). iOS always ships the engine.
- **Download rules**: only on a tap; unmetered network unless *Allow mobile data for this
  download* (reset each time); never roaming; never on mobile data with Data Saver (Android)
  or in Low Data Mode (iOS); resumes; space checked first, size and SHA-256 at the end. A Data
  Saver exemption the user gave Ichor allows mobile data without a warning (the plan wanted
  one).
- **iOS resume data** from a download allowed mobile data is reused only when the new
  download allows it too.
- **Gemma left out**: its terms carry obligations for distributors; Qwen3.5 is Apache 2.0.
- **Not done from the plan**: CPU variants beyond dot-product (i8mm), GPU on Android
  (OpenCL/Vulkan), a Live Activity for the iOS download, a catalog served from release
  assets.

## Open points

- Whether the full diagnosis should be offered to a phone model at all, or always routed to
  another provider when the report is long (today: asked, and "too long" when it does not
  fit).
- The Ukrainian answers of small models (the prompt asks for the app's language).
- Roaming cannot be told apart on iOS once mobile data is allowed (only "expensive").
