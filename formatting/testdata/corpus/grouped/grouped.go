package grouped

import (
	"bytes"
	"context"

	"github.com/go-openapi/swag/conv"

	"gopkg.in/yaml.v3"
)

// Marshal reaches one import from every group.
func Marshal(ctx context.Context, in any) ([]byte, error) {
	_ = ctx
	_ = conv.Pointer(1)
	var buf bytes.Buffer
	_ = buf
	return yaml.Marshal(in)
}
