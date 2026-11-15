package name.levis.ichor.data

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import name.levis.ichor.model.AmAlert
import name.levis.ichor.model.CheckupFinding
import name.levis.ichor.model.KubePod

// What "Explain with AI" sends Go to say which subject (see CollectExplanation), and the note
// "Continue in Diagnosis" writes for it. Notes are English, like healthCheckNote: the answer's
// language is set apart.

fun podExplainSubject(pod: KubePod): JsonObject = buildJsonObject {
    put("namespace", pod.namespace)
    put("name", pod.name)
}

fun podExplainNote(pod: KubePod): String = "Why is pod ${pod.namespace}/${pod.name} ${pod.status.ifEmpty { "not running" }}?"

/** [sourceJson]: the Alertmanager the Alerts screen reads, as AlertmanagerAlerts takes it. */
fun alertExplainSubject(alert: AmAlert, sourceJson: String): JsonObject = buildJsonObject {
    put("source", sourceJson)
    put("fingerprint", alert.fingerprint)
}

fun alertExplainNote(alert: AmAlert): String = "Why is the alert ${alert.alertname} firing?"

/** [text] and [fix]: the finding's title and fix as the app words them. */
fun findingExplainSubject(finding: CheckupFinding, text: String, fix: String): JsonObject = buildJsonObject {
    put("finding", TalosJson.encodeToJsonElement(CheckupFinding.serializer(), finding))
    put("text", text)
    put("fix", fix)
}

fun findingExplainNote(text: String): String = "The checkup found: $text"
