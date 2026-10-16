package plugin

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// Compiled is a plugin that validated: its expressions are parsed (they live in the Plugin's
// Expr and Template values), and what the runtime walks is resolved and in file order.
type Compiled struct {
	Plugin *Plugin
	// Hash is the sha256 of the source, "" for a plugin built in Go.
	Hash string
	// Resources are the resource keys, sorted.
	Resources []string
	// Fields are each resource's fields in file order (a field may use the earlier ones).
	Fields map[string][]NamedExpr
	// DataServices are in file order (one may use the earlier ones).
	DataServices []NamedDataService
	// Cards are the summary first, then the list cards in file order.
	Cards []NamedCard
	// Actions are sorted by id.
	Actions []NamedAction
	// Warnings are the validator's non-blocking problems.
	Warnings []Problem
}

// NamedExpr is a field.
type NamedExpr struct {
	Name string
	Expr Expr
}

// NamedDataService is a data service.
type NamedDataService struct {
	Name string
	DataService
}

// NamedAction is an action with its id.
type NamedAction struct {
	Name string
	Action
}

// NamedCard is a card with its source resolved: From is a resource (Resource set) or a data
// service (DataService set); both empty for the summary.
type NamedCard struct {
	Name        string
	Resource    string
	DataService string
	Card
}

// ValidationError is a plugin with error problems; it does not compile.
type ValidationError struct {
	File     string
	Problems []Problem
}

func (e *ValidationError) Error() string {
	var b strings.Builder

	for i, p := range e.Problems {
		if i > 0 {
			b.WriteByte('\n')
		}

		fmt.Fprintf(&b, "%s:%s", e.File, p)
	}

	return b.String()
}

// CompileSource loads, validates and compiles a plugin file. The same name and bytes compile
// once: the result is cached by their sha256. The returned plugin is shared and read-only.
func CompileSource(name string, src []byte) (*Compiled, error) {
	key := sha256.Sum256(append([]byte(name+"\x00"), src...))

	if c, ok := compiled.get(hex.EncodeToString(key[:])); ok {
		return c, nil
	}

	p, err := Load(name, src)
	if err != nil {
		return nil, err
	}

	c, err := Compile(p)
	if err != nil {
		return nil, err
	}

	compiled.put(hex.EncodeToString(key[:]), c)

	return c, nil
}

// Compile validates p and resolves it for the runtime; it does not cache (p is the
// caller's, and may change): CompileSource does.
func Compile(p *Plugin) (*Compiled, error) {
	problems := Validate(p)
	if HasErrors(problems) {
		return nil, &ValidationError{File: p.src.name, Problems: problems}
	}

	c := &Compiled{
		Plugin:    p,
		Hash:      p.src.hash,
		Resources: sortedKeys(p.Resources),
		Fields:    map[string][]NamedExpr{},
		Warnings:  problems,
	}

	for _, res := range keys(p.src, "fields", p.Fields) {
		for _, name := range keys(p.src, "fields."+res, p.Fields[res]) {
			c.Fields[res] = append(c.Fields[res], NamedExpr{Name: name, Expr: p.Fields[res][name]})
		}
	}

	for _, name := range keys(p.src, "dataServices", p.DataServices) {
		c.DataServices = append(c.DataServices, NamedDataService{Name: name, DataService: p.DataServices[name]})
	}

	c.Cards = append(c.Cards, NamedCard{Name: "summary", Card: p.Cards["summary"]})

	for _, name := range keys(p.src, "cards", p.Cards) {
		if name == "summary" {
			continue
		}

		// As Validate resolves it: dataServices.<name>, else a resource, else a bare data service.
		card := NamedCard{Name: name, Card: p.Cards[name]}
		if ds, ok := strings.CutPrefix(card.From, "dataServices."); ok {
			card.DataService = ds
		} else if _, isResource := p.Resources[card.From]; isResource {
			card.Resource = card.From
		} else {
			card.DataService = card.From
		}

		c.Cards = append(c.Cards, card)
	}

	for _, id := range sortedKeys(p.Actions) {
		c.Actions = append(c.Actions, NamedAction{Name: id, Action: p.Actions[id]})
	}

	return c, nil
}

// compiled is CompileSource's cache: at most 64 plugins, least recently used out first.
var compiled = newCache(64)

type cache struct {
	mu    sync.Mutex
	size  int
	order *list.List // of cacheEntry, most recent first
	byKey map[string]*list.Element
}

type cacheEntry struct {
	key string
	c   *Compiled
}

func newCache(size int) *cache {
	return &cache{size: size, order: list.New(), byKey: map[string]*list.Element{}}
}

func (c *cache) get(key string) (*Compiled, bool) {
	if key == "" {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.byKey[key]
	if !ok {
		return nil, false
	}

	c.order.MoveToFront(el)

	return el.Value.(cacheEntry).c, true
}

func (c *cache) put(key string, v *Compiled) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.byKey[key]; ok {
		el.Value = cacheEntry{key: key, c: v}
		c.order.MoveToFront(el)

		return
	}

	c.byKey[key] = c.order.PushFront(cacheEntry{key: key, c: v})

	for c.order.Len() > c.size {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.byKey, last.Value.(cacheEntry).key)
	}
}
