package name.levis.ichor.localmodel

import android.os.Build
import name.levis.ichor.BuildConfig
import java.io.File

/** Receives the answer's pieces from the engine, as UTF-8 bytes (see ichor_llm_jni.cpp). */
fun interface PieceSink {
    fun onPiece(bytes: ByteArray)
}

/** Why the on-device model cannot run on this phone, or [AVAILABLE]. */
enum class EngineStatus { AVAILABLE, NOT_IN_BUILD, NEEDS_64_BIT, OLD_CPU, LOAD_FAILED }

/**
 * libichorllm.so: llama.cpp and the engine of native/llm (built by build.sh). Only loaded once
 * the user asks the model on the phone, and only where it can run.
 */
object LlamaNative {
    val status: EngineStatus by lazy {
        when {
            !BuildConfig.LOCAL_MODEL -> EngineStatus.NOT_IN_BUILD
            Build.SUPPORTED_64_BIT_ABIS.none { it == "arm64-v8a" || it == "x86_64" } -> EngineStatus.NEEDS_64_BIT
            // The arm64 library uses the dot-product instructions (build.sh): without them it would crash.
            Build.SUPPORTED_64_BIT_ABIS.first() == "arm64-v8a" && !hasDotProd() -> EngineStatus.OLD_CPU
            runCatching { System.loadLibrary("ichorllm") }.isFailure -> EngineStatus.LOAD_FAILED
            else -> EngineStatus.AVAILABLE
        }
    }

    val available: Boolean get() = status == EngineStatus.AVAILABLE

    private fun hasDotProd(): Boolean =
        runCatching { File("/proc/cpuinfo").readLines() }.getOrDefault(emptyList())
            .filter { it.startsWith("Features") }
            .any { line -> line.substringAfter(':').split(' ').any { it == "asimddp" } }

    /** A handle, or 0 with the reason in error[0] ("out-of-memory" or a message). */
    @JvmStatic external fun load(path: String, contextTokens: Int, threads: Int, error: Array<String?>): Long

    /** The outcome in the Go core's words (see LocalModel in go/ichorgo/ai_device.go). */
    @JvmStatic external fun generate(
        handle: Long,
        roles: Array<String>,
        contents: Array<ByteArray>,
        assistantPrefix: ByteArray,
        answerTokens: Int,
        temperature: Float,
        sink: PieceSink,
    ): String

    @JvmStatic external fun cancel(handle: Long)

    @JvmStatic external fun free(handle: Long)
}
