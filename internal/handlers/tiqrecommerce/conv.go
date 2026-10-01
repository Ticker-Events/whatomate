package tiqrecommerce

import "github.com/shridarpatil/whatomate/internal/handlers/codedflow"

// Conv wraps codedflow.Conv so this package can attach ecommerce methods.
type Conv struct {
	*codedflow.Conv
}

func Wrap(c *codedflow.Conv) *Conv { return wrap(c) }

func wrap(c *codedflow.Conv) *Conv {
	if c == nil {
		return nil
	}
	return &Conv{Conv: c}
}

func unwrap(c *Conv) *codedflow.Conv {
	if c == nil {
		return nil
	}
	return c.Conv
}
