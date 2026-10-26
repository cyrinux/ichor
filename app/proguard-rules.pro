# gomobile bindings are called through JNI. The Go core binds to name.levis.ichorgo
# (-javapkg=name.levis), apart from the app's own name.levis.ichor, so keep that whole package.
-keep class go.** { *; }
-keep class name.levis.ichorgo.** { *; }

# The on-device model engine (libichorllm.so) binds by name: LlamaNative's natives, and the
# onPiece([B)V it calls back on the sink.
-keep class name.levis.ichor.localmodel.LlamaNative { native <methods>; }
-keep interface name.levis.ichor.localmodel.PieceSink { *; }
-keepclassmembers class * implements name.levis.ichor.localmodel.PieceSink { void onPiece(byte[]); }
