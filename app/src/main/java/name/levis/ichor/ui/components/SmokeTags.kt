package name.levis.ichor.ui.components

import name.levis.ichor.ui.node.NodeMenuEntry

// The anchors the demo-mode smoke test (androidTest DemoSmokeTest) waits for and taps. Test
// tags are not shown or read out; when a screen moves an anchor, move its tag, not the test.

/** An overview node row (or chip). */
const val SMOKE_NODE = "smoke-node"

/** The node screen's list (its header and menu). */
const val SMOKE_NODE_SCREEN = "smoke-node-screen"

/** The log lines of a logs screen. */
const val SMOKE_LOG_LINES = "smoke-log-lines"

/** The Kubernetes home of a cluster added from a kubeconfig. */
const val SMOKE_KUBE_HOME = "smoke-kube-home"

/** A node screen's menu entry. */
fun smokeMenuTag(entry: NodeMenuEntry) = "smoke-node-menu-${entry.name}"
