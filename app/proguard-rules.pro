# gomobile bindings are called through JNI. The Go core binds to name.levis.ichorgo
# (-javapkg=name.levis), apart from the app's own name.levis.ichor, so keep that whole package.
-keep class go.** { *; }
-keep class name.levis.ichorgo.** { *; }
