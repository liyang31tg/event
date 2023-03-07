package event

import (
	"testing"
	"time"
)

func TestClient(t *testing.T) {
	time.Sleep(1e9)
	if err := defaultClient.On("dd", func(a int) error {
		t.Log("ddddddddddddddddhandleaction")
		return nil
	}); err != nil {
		t.Error(err)
	}
	if err := defaultClient.Emit("dd", 1, 2, 3, "5"); err != nil {
		t.Error(err)
	}

	t.Error("dd")
}
