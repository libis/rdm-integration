// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package types

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsUnrecoverable(t *testing.T) {
	base := errors.New("could not open the file")
	unrecoverable := NewUnrecoverableError(base)

	if !IsUnrecoverable(unrecoverable) {
		t.Error("expected a wrapped error to be detected as unrecoverable")
	}
	if !IsUnrecoverable(fmt.Errorf("job failed: %w", unrecoverable)) {
		t.Error("expected detection through further wrapping")
	}
	if IsUnrecoverable(base) {
		t.Error("expected a plain error to not be unrecoverable")
	}
	if IsUnrecoverable(nil) {
		t.Error("expected nil to not be unrecoverable")
	}
	if !errors.Is(unrecoverable, base) {
		t.Error("expected the original error to remain reachable via Unwrap")
	}
}
