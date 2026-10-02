# gomobile bindings are called through JNI. With -javapkg=name.levis they share the app's
# root package, so keep only the generated classes, not the whole package.
-keep class go.** { *; }
-keep class name.levis.talosmobile.Talosmobile { *; }
-keep class name.levis.talosmobile.HealthListener { *; }
-keep class name.levis.talosmobile.HealthRun { *; }
-keep class name.levis.talosmobile.DebugListener { *; }
-keep class name.levis.talosmobile.DebugSession { *; }

# ML Kit (QR import) instantiates its components by reflection from manifest metadata; R8 full
# mode stripped their constructors and BarcodeScanning.getClient() crashed in release builds
# only. Keeping the registrars is enough: ML Kit's own consumer rules cover the rest, and
# keeping all of com.google.mlkit.** left a third of the DEX unoptimized.
-keep class * implements com.google.firebase.components.ComponentRegistrar { <init>(); *; }

# Batch 4 bindings (packet capture, upgrade): Go calls back into these through JNI.
-keep class name.levis.talosmobile.CaptureListener { *; }
-keep class name.levis.talosmobile.CaptureRun { *; }
-keep class name.levis.talosmobile.UpgradeListener { *; }
-keep class name.levis.talosmobile.UpgradeRun { *; }
