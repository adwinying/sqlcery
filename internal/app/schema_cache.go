package app

import "sync"

type autocompleteSchemaCache struct {
	mu     sync.RWMutex
	schema *AutocompleteSchemaContext
}

func newAutocompleteSchemaCache() *autocompleteSchemaCache {
	return &autocompleteSchemaCache{}
}

func (c *autocompleteSchemaCache) Replace(schema *AutocompleteSchemaContext) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.schema = cloneAutocompleteSchemaContext(schema)
}

func (c *autocompleteSchemaCache) Snapshot() *AutocompleteSchemaContext {
	if c == nil {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return cloneAutocompleteSchemaContext(c.schema)
}
