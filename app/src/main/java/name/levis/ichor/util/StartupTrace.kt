package name.levis.ichor.util

import android.os.Process
import android.os.SystemClock
import android.util.Log
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.int
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
import name.levis.ichorgo.Ichorgo
import name.levis.ichorgo.TraceListener
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

/**
 * Where a cold start's time goes, in logcat (`adb logcat -s Ichor.Startup`): one line per
 * step with the time since the process started and since the previous step, the Go core's
 * steps (config parse, session, first node, all nodes) in the same stream.
 *
 * Off unless the "Startup timing in logcat" setting is on or the build is a debug one. The
 * lines carry step names, counts and durations only: never a context name, an endpoint or an
 * address (the privacy mask does not cover logcat). This is the app's only `Log` call.
 */
object StartupTrace {
    private const val TAG = "Ichor.Startup"

    @Volatile
    private var enabled = false
    private val previous = AtomicLong(0)
    private val marked = ConcurrentHashMap.newKeySet<String>()

    /** Turns the trace on or off, the Go core's listener with it (none is set while off). */
    fun setEnabled(on: Boolean) {
        if (on == enabled) return
        enabled = on
        Ichorgo.setTraceListener(if (on) GoSteps else null)
    }

    /** Logs [step] now, when the trace is on. */
    fun mark(step: String) {
        if (!enabled) return
        val now = SystemClock.elapsedRealtime() - Process.getStartElapsedRealtime()
        Log.i(TAG, line(step, now, now - previous.getAndSet(now)))
    }

    /**
     * [mark] the first time [key] is seen in this process only: a step of the cold start that
     * runs again later (a refresh, the monitor's read) logs once. [detail] follows the key.
     */
    fun markOnce(key: String, detail: String = "") {
        if (!enabled || !marked.add(key)) return
        mark(if (detail.isEmpty()) key else "$key $detail")
    }

    /** "<step> +<ms since process start> (Δ<ms since the previous step>)". */
    internal fun line(step: String, sinceStart: Long, delta: Long): String = "$step +${sinceStart}ms (Δ${delta}ms)"

    /**
     * A Go trace step ({"step","ms","at","count","unreachable"}) as a step name for [mark]:
     * "go:overview.all_nodes 8003ms count=8 unreachable=1". Anything else is kept as it came.
     */
    internal fun goStep(json: String): String = runCatching {
        val o = Json.parseToJsonElement(json).jsonObject
        buildString {
            append("go:").append(o.getValue("step").jsonPrimitive.content)
            append(' ').append(o.getValue("ms").jsonPrimitive.long).append("ms")
            o["count"]?.jsonPrimitive?.intOrNull?.let { append(" count=").append(it) }
            o["unreachable"]?.jsonPrimitive?.int?.takeIf { it > 0 }?.let { append(" unreachable=").append(it) }
        }
    }.getOrElse { "go:$json" }

    /** Called on Go threads: logs right away, no hop to another thread. */
    private object GoSteps : TraceListener {
        override fun onTrace(json: String) = mark(goStep(json))
    }
}
