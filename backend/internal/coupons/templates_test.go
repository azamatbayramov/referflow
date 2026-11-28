package coupons

import (
	"strings"
	"testing"
)

func TestTemplateValidationUsesCouponRules(t *testing.T) {
	one := 1
	base := TemplateInput{Name: "  Близким  ", DiscountType: "fixed", Value: "200,25", Restrictions: "  Условия\nполностью  ", PerClientLimit: &one}
	in, value, err := normalizeTemplate(base)
	if err != nil || in.Name != "Близким" || in.Value != "200.25" || value != 20025 || in.Restrictions != "Условия\nполностью" {
		t.Fatal(in, value, err)
	}
	for _, change := range []func(*TemplateInput){
		func(in *TemplateInput) { in.Name = "   " },
		func(in *TemplateInput) { in.Name = "Купон\nдругу" },
		func(in *TemplateInput) { in.Name = "Купон\u200b" },
		func(in *TemplateInput) { in.Name = strings.Repeat("я", 121) },
		func(in *TemplateInput) { in.DiscountType = "other" },
		func(in *TemplateInput) { in.DiscountType = "percent"; in.Value = "100.01" },
		func(in *TemplateInput) { in.Value = "0" },
		func(in *TemplateInput) { in.Value = "1.001" },
		func(in *TemplateInput) { zero := 0; in.TotalLimit = &zero },
		func(in *TemplateInput) { negative := -1; in.PerClientLimit = &negative },
		func(in *TemplateInput) { date := "2026-02-30"; in.ExpiresOn = &date },
		func(in *TemplateInput) { in.Restrictions = strings.Repeat("я", 2001) },
	} {
		bad := base
		change(&bad)
		if _, _, err := normalizeTemplate(bad); err != ErrInvalid {
			t.Fatal("invalid template accepted", bad, err)
		}
	}
	base.Name = strings.Repeat("я", 120)
	base.PerClientLimit = nil
	date := "2020-01-01"
	base.ExpiresOn = &date
	if _, _, err := normalizeTemplate(base); err != nil {
		t.Fatal("unlimited or historical date rejected", err)
	}
}
