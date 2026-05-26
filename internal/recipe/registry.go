package recipe

import (
	"github.com/UberMorgott/morgue/internal/recon"
)

var registry []Recipe

// Register appends a recipe to the global registry.
func Register(r Recipe) {
	registry = append(registry, r)
}

// RegisterFirst prepends a recipe to the global registry (for priority matching).
func RegisterFirst(r Recipe) {
	registry = append([]Recipe{r}, registry...)
}

// Match returns the first recipe that matches the given recon result.
func Match(r *recon.Result) Recipe {
	for _, rec := range registry {
		if rec.Match(r) {
			return rec
		}
	}
	return nil
}

// All returns all registered recipes.
func All() []Recipe {
	return registry
}

// FindByName returns a recipe by its name.
func FindByName(name string) Recipe {
	for _, r := range registry {
		if r.Name() == name {
			return r
		}
	}
	return nil
}
