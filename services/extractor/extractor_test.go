package main

import (
	"context"
	"testing"
)

func TestExtractTextForUnsupportedContentIsImpossible(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()
	_, err := extractText(ctx, "/tmp/test", "application/yaml")
	if err == nil || err.Error() != `unsupported content type "application/yaml"` {
		t.Errorf(`Errored: %q`, err)
	}
}
