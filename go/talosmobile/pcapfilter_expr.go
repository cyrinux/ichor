package talosmobile

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/bpf"
)

// Capture filters are compiled here to classic BPF, in pure Go (no libpcap): an expression
// is parsed into a tree of and/or/not over tests (load a packet field, compare it), and the
// tree is laid out with jumps to labels resolved at the end. Like tcpdump, "not" binds
// tightest and "and"/"or" have equal precedence, left to right.

type filterNode interface{}

type (
	filterAnd  struct{ l, r filterNode }
	filterOr   struct{ l, r filterNode }
	filterNot  struct{ x filterNode }
	filterTest struct {
		load []bpf.Instruction // computes the value into A
		cond bpf.JumpTest
		val  uint32
	}
	filterConst bool
)

const bpfMatch = 0x40000 // what libpcap returns for a match: the whole packet

func and(nodes ...filterNode) filterNode {
	out := nodes[0]
	for _, n := range nodes[1:] {
		out = filterAnd{out, n}
	}

	return out
}

func or(nodes ...filterNode) filterNode {
	out := nodes[0]
	for _, n := range nodes[1:] {
		out = filterOr{out, n}
	}

	return out
}

type filterParser struct {
	tokens []string
	pos    int
}

func tokenizeFilter(expr string) []string {
	return strings.Fields(strings.NewReplacer("(", " ( ", ")", " ) ").Replace(expr))
}

func (p *filterParser) peek() string {
	if p.pos < len(p.tokens) {
		return strings.ToLower(p.tokens[p.pos])
	}

	return ""
}

func isFilterOperator(tok string) bool {
	switch tok {
	case "and", "or", "not", "&&", "||", "!", "(", ")":
		return true
	}

	return false
}

func (p *filterParser) parseExpr() (filterNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for {
		op := p.peek()

		switch op {
		case "", ")":
			return left, nil
		case "and", "&&", "or", "||":
			p.pos++
		default:
			return nil, fmt.Errorf("expected \"and\" or \"or\" before %q", op)
		}

		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}

		if op == "and" || op == "&&" {
			left = filterAnd{left, right}
		} else {
			left = filterOr{left, right}
		}
	}
}

func (p *filterParser) parseUnary() (filterNode, error) {
	switch tok := p.peek(); tok {
	case "":
		return nil, errors.New("incomplete filter expression")
	case "not", "!":
		p.pos++

		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}

		return filterNot{x}, nil
	case "(":
		p.pos++

		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}

		if p.peek() != ")" {
			return nil, errors.New("unbalanced parentheses")
		}

		p.pos++

		return x, nil
	case ")", "and", "or", "&&", "||":
		return nil, fmt.Errorf("unexpected %q", tok)
	default:
		start := p.pos
		for p.pos < len(p.tokens) && !isFilterOperator(p.peek()) {
			p.pos++
		}

		return parsePrimitive(p.tokens[start:p.pos])
	}
}

// parseFilter parses and compiles a whole expression.
func parseFilter(expr string) ([]bpf.Instruction, error) {
	p := &filterParser{tokens: tokenizeFilter(expr)}

	root, err := p.parseExpr()
	if err != nil {
		return nil, err
	}

	if p.pos != len(p.tokens) {
		return nil, fmt.Errorf("unexpected %q", p.peek())
	}

	return emitFilter(root), nil
}

// filterEmitter lays out the program; jumps to labels are resolved at the end.
type filterEmitter struct {
	out    []bpf.Instruction
	jumps  map[int]int // instruction index -> label
	labels []int       // label -> instruction index
}

func (e *filterEmitter) newLabel() int {
	e.labels = append(e.labels, -1)

	return len(e.labels) - 1
}

func (e *filterEmitter) place(label int) { e.labels[label] = len(e.out) }

func (e *filterEmitter) jumpTo(label int) {
	e.jumps[len(e.out)] = label
	e.out = append(e.out, bpf.Jump{})
}

func (e *filterEmitter) emit(n filterNode, onTrue, onFalse int) {
	switch v := n.(type) {
	case filterAnd:
		mid := e.newLabel()
		e.emit(v.l, mid, onFalse)
		e.place(mid)
		e.emit(v.r, onTrue, onFalse)
	case filterOr:
		mid := e.newLabel()
		e.emit(v.l, onTrue, mid)
		e.place(mid)
		e.emit(v.r, onTrue, onFalse)
	case filterNot:
		e.emit(v.x, onFalse, onTrue)
	case filterConst:
		if v {
			e.jumpTo(onTrue)
		} else {
			e.jumpTo(onFalse)
		}
	case filterTest:
		// Conditional jumps only reach 255 instructions: test, then two long jumps.
		e.out = append(e.out, v.load...)
		e.out = append(e.out, bpf.JumpIf{Cond: v.cond, Val: v.val, SkipTrue: 0, SkipFalse: 1})
		e.jumpTo(onTrue)
		e.jumpTo(onFalse)
	}
}

// emitFilter turns the parsed expression into one program ending in match/drop returns.
func emitFilter(root filterNode) []bpf.Instruction {
	e := &filterEmitter{jumps: map[int]int{}}
	match, drop := e.newLabel(), e.newLabel()

	e.emit(root, match, drop)
	e.place(match)
	e.out = append(e.out, bpf.RetConstant{Val: bpfMatch})
	e.place(drop)
	e.out = append(e.out, bpf.RetConstant{Val: 0})

	for at, label := range e.jumps {
		e.out[at] = bpf.Jump{Skip: uint32(e.labels[label] - at - 1)}
	}

	return e.out
}
