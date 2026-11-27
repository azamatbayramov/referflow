package coupons

import (
	"strings"
	"testing"
	"time"
)

func TestValuesCodesAndExpiry(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{{"0,01", 1}, {"12.5", 1250}, {"100", 10000}} {
		v, err := parseValue(tc.in)
		if err != nil || v != tc.want {
			t.Fatal(tc, err)
		}
	}
	for _, v := range []string{"0", "-1", "NaN", "1e2", "1.001", "99999999999"} {
		if _, err := parseValue(v); err == nil {
			t.Fatal("invalid decimal", v)
		}
	}
	for _, tc := range []struct{ in, want string }{{"12", "000012"}, {" 12-34 56 ", "123456"}, {"000000", "000000"}} {
		v, err := normalizeCode(tc.in)
		if err != nil || v != tc.want {
			t.Fatal(tc, v, err)
		}
	}
	for _, v := range []string{"", "1234567", "12a", "１２３"} {
		if _, err := normalizeCode(v); err == nil {
			t.Fatal("invalid code", v)
		}
	}
	day := "2026-11-27"
	one := 1
	in, v, end, err := normalize(Input{DiscountType: "percent", Value: "99,99", PerClientLimit: &one, ExpiresOn: &day})
	if err != nil || v != 9999 || in.Value != "99.99" || !end.Equal(time.Date(2026, 11, 27, 21, 0, 0, 0, time.UTC)) {
		t.Fatal(in, v, end, err)
	}
	zero := 0
	badDay := "2026-02-30"
	for _, bad := range []Input{{DiscountType: "percent", Value: "100.01"}, {DiscountType: "fixed", Value: "1", TotalLimit: &zero}, {DiscountType: "fixed", Value: "1", ExpiresOn: &badDay}, {DiscountType: "fixed", Value: "1", Restrictions: strings.Repeat("я", 2001)}} {
		if _, _, _, err := normalize(bad); err == nil {
			t.Fatal("invalid coupon accepted")
		}
	}
}
