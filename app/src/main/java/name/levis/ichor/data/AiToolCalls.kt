package name.levis.ichor.data

import kotlinx.serialization.Serializable

/** What the model named in a request, as Go reports it: the placeholders it used, never decided here. */
@Serializable
data class AiToolArgs(
    val namespace: String = "",
    val pod: String = "",
    val kind: String = "",
    val name: String = "",
    val node: String = "",
    val service: String = "",
)

/** One request of the model, and once it is [done], what went back: [result] is the exact text sent. */
data class AiToolCall(
    val tool: String,
    val args: AiToolArgs,
    val done: Boolean = false,
    val bytes: Long = 0,
    val result: String = "",
    /** Why nothing was read. */
    val error: String? = null,
)

/** [AnswerEvent.ToolDone] applied to [calls]: it closes the oldest request of that tool still open. */
fun List<AiToolCall>.finished(event: AnswerEvent.ToolDone): List<AiToolCall> {
    val index = indexOfFirst { !it.done && it.tool == event.tool }
    if (index < 0) return this
    return mapIndexed { i, call ->
        if (i == index) call.copy(done = true, bytes = event.bytes, result = event.result, error = event.error) else call
    }
}
