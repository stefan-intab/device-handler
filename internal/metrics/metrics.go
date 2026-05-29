package metrics

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

type Registry struct {
	mu      sync.RWMutex
	metrics map[string]metric
}

type metric interface {
	Name() string
	Type() string
	Help() string
	ValueString() string
}

func NewRegistry() *Registry {
	return &Registry{
		metrics: make(map[string]metric),
	}
}

func (r *Registry) NewCounter(name, help string) *Counter {
	counter := &Counter{name: name, help: help}
	r.register(counter)
	return counter
}

func (r *Registry) NewGauge(name, help string) *Gauge {
	gauge := &Gauge{name: name, help: help}
	r.register(gauge)
	return gauge
}

func (r *Registry) register(m metric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics[m.Name()] = m
}

func (r *Registry) RenderPrometheus() string {
	r.mu.RLock()
	names := make([]string, 0, len(r.metrics))
	for name := range r.metrics {
		names = append(names, name)
	}
	slices.Sort(names)

	ordered := make([]metric, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, r.metrics[name])
	}
	r.mu.RUnlock()

	var b strings.Builder
	for _, metric := range ordered {
		fmt.Fprintf(&b, "# HELP %s %s\n", metric.Name(), metric.Help())
		fmt.Fprintf(&b, "# TYPE %s %s\n", metric.Name(), metric.Type())
		fmt.Fprintf(&b, "%s %s\n", metric.Name(), metric.ValueString())
	}
	return b.String()
}

func (r *Registry) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(r.RenderPrometheus()))
	}
}

type Counter struct {
	name  string
	help  string
	value atomic.Uint64
}

func (c *Counter) Name() string        { return c.name }
func (c *Counter) Type() string        { return "counter" }
func (c *Counter) Help() string        { return c.help }
func (c *Counter) ValueString() string { return fmt.Sprintf("%d", c.value.Load()) }
func (c *Counter) Inc()                { c.value.Add(1) }
func (c *Counter) Add(delta uint64)    { c.value.Add(delta) }

type Gauge struct {
	name  string
	help  string
	value atomic.Int64
}

func (g *Gauge) Name() string        { return g.name }
func (g *Gauge) Type() string        { return "gauge" }
func (g *Gauge) Help() string        { return g.help }
func (g *Gauge) ValueString() string { return fmt.Sprintf("%d", g.value.Load()) }
func (g *Gauge) Set(value int64)     { g.value.Store(value) }
