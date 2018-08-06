package oxipay

import (
	"testing"
)

func TestProcessAuthorisationResponse(t *testing.T) {
	x := ProcessAuthorisationResponses()("EVAL02")
	if x == nil || x.TxnStatus != StatusFailed {
		t.Errorf("unexpected response: got %v want %v", x.TxnStatus, StatusFailed)
	}

	y := ProcessAuthorisationResponses()("SPRA01")
	if y == nil || y.TxnStatus != StatusApproved {
		t.Errorf("unexpected response: got %v want %v", y.TxnStatus, StatusFailed)
	}
}
