# gomobile bindings are called through JNI. The Go core binds to name.levis.ichorgo
# (-javapkg=name.levis), apart from the app's own name.levis.ichor, so keep that whole package.
-keep class go.** { *; }
-keep class name.levis.ichorgo.** { *; }

# ML Kit (QR import) instantiates its components by reflection from manifest metadata; R8 full
# mode stripped their constructors and BarcodeScanning.getClient() crashed in release builds
# only. Keeping the registrars is enough: ML Kit's own consumer rules cover the rest, and
# keeping all of com.google.mlkit.** left a third of the DEX unoptimized.
-keep class * implements com.google.firebase.components.ComponentRegistrar { <init>(); *; }
