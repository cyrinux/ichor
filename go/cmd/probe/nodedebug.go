package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// nodeDebugProbe runs one command in a node debug shell once it opens, without a terminal:
// it checks scheduling, Pod Security Admission and nsenter on a real cluster.
type nodeDebugProbe struct {
	session *ichorgo.DebugSession
	ready   chan struct{}
	once    sync.Once
	mu      sync.Mutex
	output  strings.Builder
	done    chan string
}

func (p *nodeDebugProbe) OnStatus(message string) {
	fmt.Fprintln(os.Stderr, message)

	if strings.HasPrefix(message, "Root shell on ") {
		p.once.Do(func() { close(p.ready) })
	}
}

func (p *nodeDebugProbe) OnOutput(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.output.Write(data)
}

func (p *nodeDebugProbe) OnExit(code int, errMessage string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done <- fmt.Sprintf("%s\nexit=%d err=%q", p.output.String(), code, errMessage)
}

// nodeDebugRun opens a root shell on node through a privileged pod in namespace ("" for
// default), runs `hostname` and `uname -a` on the host, and exits: the pod is deleted before
// the exit is reported.
func nodeDebugRun(cfg, contextName, kubeServer, node, namespace, image string) string {
	p := &nodeDebugProbe{ready: make(chan struct{}), done: make(chan string, 1)}
	p.session = ichorgo.StartNodeDebug(cfg, contextName, kubeServer, node, namespace, image, 120, 40, p)

	select {
	case <-p.ready:
		p.session.Write([]byte("hostname; uname -a; exit\r"))
	case out := <-p.done:
		return out
	}

	return <-p.done
}
