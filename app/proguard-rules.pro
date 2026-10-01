# gomobile bindings are called through JNI. With -javapkg=name.levis they share the app's
# root package, so keep only the generated classes, not the whole package.
-keep class go.** { *; }
-keep class name.levis.talosmobile.Talosmobile { *; }
-keep class name.levis.talosmobile.HealthListener { *; }
-keep class name.levis.talosmobile.HealthRun { *; }
-keep class name.levis.talosmobile.DebugListener { *; }
-keep class name.levis.talosmobile.DebugSession { *; }

# ML Kit (QR import) instantiates its components by reflection from manifest metadata; R8 full
# mode stripped what it needed and BarcodeScanning.getClient() crashed in release builds only.
-keep class * implements com.google.firebase.components.ComponentRegistrar { <init>(); *; }
-keep class com.google.mlkit.** { *; }
-keep class com.google.android.gms.internal.mlkit_vision_barcode.** { *; }
-keep class com.google.android.gms.internal.mlkit_vision_common.** { *; }
-keep class com.google.android.gms.internal.mlkit_common.** { *; }
