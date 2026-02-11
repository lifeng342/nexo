package common

import "testing"

func TestGeneratePasswordFromUserId(t *testing.T) {
	uId := "u___12345678923123"
	v := GeneratePasswordFromUserId(uId, "test", 10)
	t.Log(v)
}
