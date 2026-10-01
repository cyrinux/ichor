# gomobile bindings are called through JNI. The Go core binds to name.levis.talosmobile
# (-javapkg=name.levis), apart from the app's own name.levis.ichor, so keep that whole package.
-keep class go.** { *; }
-keep class name.levis.talosmobile.** { *; }

# ML Kit (QR import) instantiates its components by reflection from manifest metadata; R8 full
# mode stripped what it needed and BarcodeScanning.getClient() crashed in release builds only.
-keep class * implements com.google.firebase.components.ComponentRegistrar { <init>(); *; }
-keep class com.google.mlkit.** { *; }
-keep class com.google.android.gms.internal.mlkit_vision_barcode.** { *; }
-keep class com.google.android.gms.internal.mlkit_vision_common.** { *; }
-keep class com.google.android.gms.internal.mlkit_common.** { *; }
