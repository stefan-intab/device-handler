package device

import "sync"

type Registry struct {
	mu      sync.RWMutex
	parsers map[string]Parser
}

func NewRegistry() *Registry {
	return &Registry{
		parsers: make(map[string]Parser),
	}
}

func (r *Registry) Register(parser Parser) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parsers[Key(parser.Manufacturer(), parser.Model())] = parser
}

func (r *Registry) Lookup(manufacturer, model string) (Parser, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	parser, ok := r.parsers[Key(manufacturer, model)]
	return parser, ok
}
