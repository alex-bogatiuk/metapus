package itrx

import "testing"

func TestOrderNotFoundMessage(t *testing.T) {
	for _, message := range []string{"Can’t find the order.", "Can't find the order", "order not found"} {
		if !orderNotFoundMessage(message) {
			t.Errorf("%q must be recognized as no such order", message)
		}
	}
	for _, message := range []string{"insufficient balance", "signature invalid", ""} {
		if orderNotFoundMessage(message) {
			t.Errorf("%q must stay ambiguous", message)
		}
	}
}
