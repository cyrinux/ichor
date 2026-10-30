package ichorgo

// The IANA time zone database, embedded (about 450 KB). Go on iOS has no zoneinfo to read:
// its time package lists no system zone sources there (zoneinfo_ios.go) and looks only for a
// zoneinfo.zip under GOROOT or the app's working directory, which a gomobile framework does
// not ship. Without this, time.LoadLocation fails for every zone and the alert rules' quiet
// hours (AlertRulesApply) could never apply on iOS. Android reads the system's tzdata, so it
// only gains a fallback.
import _ "time/tzdata"
