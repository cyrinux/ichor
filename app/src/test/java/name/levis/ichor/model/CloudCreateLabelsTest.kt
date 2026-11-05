package name.levis.ichor.model

import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

/** The Omni labels chosen on the card reach every create's specJSON as labelsJson. */
class CloudCreateLabelsTest {
    private val labels = omniLabelsJson("controlplane", "rack=b2")

    private fun field(json: String) = TalosJson.parseToJsonElement(json).jsonObject["labelsJson"]

    @Test
    fun theLabelsTravelAsOneJsonString() {
        val specs = listOf(
            TalosJson.encodeToString(HetznerCreateSpec.serializer(), HetznerCreateSpec("cx22", "fsn1", 7, "node-c", labelsJson = labels)),
            TalosJson.encodeToString(OVHCreateSpec.serializer(), OVHCreateSpec("GRA11", "a", "t", "node-c", labelsJson = labels)),
            TalosJson.encodeToString(AWSCreateSpec.serializer(), AWSCreateSpec("eu-west-3", "t3.small", "ami-1", "subnet-1", hostname = "node-c", labelsJson = labels)),
            TalosJson.encodeToString(
                AzureCreateSpec.serializer(),
                AzureCreateSpec("rg", "westeurope", "Standard_B2s", "img", "subnet", hostname = "node-c", labelsJson = labels),
            ),
        )
        specs.forEach { json ->
            assertEquals(json, labels, field(json)?.jsonPrimitive?.content)
            val decoded = TalosJson.parseToJsonElement(field(json)!!.jsonPrimitive.content).jsonObject
            assertEquals("controlplane", decoded["ichor/role"]?.jsonPrimitive?.content)
            assertEquals("b2", decoded["rack"]?.jsonPrimitive?.content)
        }
    }

    @Test
    fun plainTalosSendsNoLabels() {
        val json = TalosJson.encodeToString(HetznerCreateSpec.serializer(), HetznerCreateSpec("cx22", "fsn1", 7, "node-c", role = "worker"))
        assertFalse(json, json.contains("labelsJson"))
    }
}
