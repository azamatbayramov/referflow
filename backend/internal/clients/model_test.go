package clients

import (
	"strings"
	"testing"
)

func TestClientValidationAndSearch(t *testing.T) {
	in, digits, err := normalize(Input{Name: "  Анна   Тестовая  ", Phone: " +7 (900) 000-00-01 "})
	if err != nil || in.Name != "Анна Тестовая" || digits != "79000000001" {
		t.Fatal(in, digits, err)
	}
	if _, _, err := normalize(Input{Name: "Без телефона"}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []Input{{Name: " "}, {Name: strings.Repeat("я", 121)}, {Name: "a\nb"}, {Name: "a\u200bb"}, {Name: "a", Phone: "123"}, {Name: "a", Phone: "call me"}, {Name: "a", Phone: "1234567+"}} {
		if _, _, err := normalize(in); err == nil {
			t.Fatal("invalid client accepted")
		}
	}
	if searchText("СЕМЁНОВ") != "семенов" {
		t.Fatal("Cyrillic search mismatch")
	}
	if validUUID("00000000-0000-4000-8000-00000000000x") || validUUID("00000000-0000-4000-8000-0000000000--") || !validUUID("00000000-0000-4000-8000-000000000001") {
		t.Fatal("invalid UUID validation")
	}
}
