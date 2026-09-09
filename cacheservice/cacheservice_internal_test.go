package cacheservice

import (
	"errors"
	"testing"
)

func TestErrorTranslationPreservesUnknownErrors(t *testing.T) {
	t.Parallel()

	marker := errors.New("marker")
	if got := translateOptionsError(marker); !errors.Is(got, marker) {
		t.Fatalf("translateOptionsError() = %v", got)
	}
	if got := translateStartupError(marker); !errors.Is(got, marker) {
		t.Fatalf("translateStartupError() = %v", got)
	}
}
