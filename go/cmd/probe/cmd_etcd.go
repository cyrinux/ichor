package main

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// etcd.
var etcdCommands = []command{
	{name: "snapshot-probe", args: "NODE", run: func(e env) (out string, err error) {
		dest := filepath.Join(os.TempDir(), "etcd-probe.snapshot")
		p := &snapshotProbe{done: make(chan string, 1)}
		if e.ageRecipient != "" {
			dest += ".age"
			p.run = ichorgo.StartEtcdSnapshotEncrypted(e.cfg, e.context, flag.Arg(1), dest, e.ageRecipient, "", p)
		} else {
			p.run = ichorgo.StartEtcdSnapshot(e.cfg, e.context, flag.Arg(1), dest, p)
		}
		out = <-p.done
		if _, statErr := os.Stat(dest + ".part"); statErr == nil {
			out += " (partial file left!)"
		}

		return out, err
	}},
	{name: "etcd", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.EtcdStatus(e.cfg, e.context)

		return out, err
	}},
	{name: "etcd-member-plan", args: "MEMBERID", run: func(e env) (out string, err error) {
		// Read-only: never removes a member.
		out, err = ichorgo.EtcdMemberPlan(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "snapshot-inspect", args: "FILE", run: func(e env) (out string, err error) {
		// Read-only: is the file encrypted, with what, its size and sha256.
		return ichorgo.SnapshotInspect(flag.Arg(1))
	}},
	{name: "etcd-recover", args: "NODE FILE [IDENTITY_FILE]", run: func(e env) (out string, err error) {
		// Bootstraps etcd on NODE from FILE for real; refused while a member answers. An
		// encrypted FILE opens with the age secret key in IDENTITY_FILE, or with the passphrase
		// in $ICHOR_SNAPSHOT_PASSPHRASE (an environment variable: not in the shell history).
		identity := ""
		if flag.Arg(3) != "" {
			b, readErr := os.ReadFile(flag.Arg(3))
			if readErr != nil {
				return "", readErr
			}

			identity = string(b)
		}

		m := maintenanceProbe{done: make(chan string, 1)}
		ichorgo.StartEtcdRecover(e.cfg, e.context, flag.Arg(1), flag.Arg(2), identity, os.Getenv("ICHOR_SNAPSHOT_PASSPHRASE"), false, m)
		out = "done: " + <-m.done

		return out, err
	}},
	{name: "etcd-nospace-fix", args: "NODE [DEST]", run: func(e env) (out string, err error) {
		// Defragments every member and disarms the alarm for real; refused without NOSPACE.
		// DEST: an absolute path for a clear snapshot first; none skips it.
		m := maintenanceProbe{done: make(chan string, 1)}
		ichorgo.StartEtcdNospaceFix(e.cfg, e.context, flag.Arg(1), flag.Arg(2), "", "", m)
		out = "done: " + <-m.done

		return out, err
	}},
}
