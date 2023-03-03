package event

import (
	"testing"
)

func TestClient(t *testing.T) {
	defalutLocalClient.On("dd", "dd")
	defalutLocalClient.GoEmit("dd", 1, 2, 3, "5")
	t.Error("dd")
}
