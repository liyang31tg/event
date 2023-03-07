package event

import (
	"bytes"
	"testing"
	"time"
)

func TestClient(t *testing.T) {
	client := NewClient(func() (Codec, error) {
		var buf bytes.Buffer
		codec := NewGobCodec(&buf)
		return codec, nil
	})
	if err := client.On("dd", func(a int) error {
		t.Log("ddddddddddddddddhandleaction")
		return nil
	}); err != nil {
		t.Error(err)
	}
	time.Sleep(1e9)
	if err := DefaultClient.Emit("dd", 1, 2, 3, "5"); err != nil {
		t.Error(err)
	}

	t.Error("dd")
}
