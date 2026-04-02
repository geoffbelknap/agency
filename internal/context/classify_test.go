package context

import (
	"testing"
)

func TestClassifySeverity(t *testing.T) {
	tests := []struct {
		name string
		old  map[string]interface{}
		new_ map[string]interface{}
		want Severity
	}{
		{
			name: "exception grant is LOW",
			old:  map[string]interface{}{},
			new_: map[string]interface{}{"granted_capabilities": []interface{}{"web_search"}},
			want: SeverityLow,
		},
		{
			name: "capability revocation is HIGH",
			old:  map[string]interface{}{"granted_capabilities": []interface{}{"web_search", "code_exec"}},
			new_: map[string]interface{}{"granted_capabilities": []interface{}{"web_search"}},
			want: SeverityHigh,
		},
		{
			name: "budget tightening is MEDIUM",
			old:  map[string]interface{}{"budget": map[string]interface{}{"max_daily_usd": 10.0}},
			new_: map[string]interface{}{"budget": map[string]interface{}{"max_daily_usd": 5.0}},
			want: SeverityMedium,
		},
		{
			name: "identical constraints is LOW",
			old:  map[string]interface{}{"foo": "bar"},
			new_: map[string]interface{}{"foo": "bar"},
			want: SeverityLow,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifySeverity(tt.old, tt.new_)
			if got != tt.want {
				t.Errorf("ClassifySeverity() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestApplyEscalation(t *testing.T) {
	t.Run("escalate LOW to HIGH", func(t *testing.T) {
		got, err := ApplyEscalation(SeverityLow, "HIGH")
		if err != nil {
			t.Fatal(err)
		}
		if got != SeverityHigh {
			t.Errorf("got %s, want HIGH", got)
		}
	})

	t.Run("downgrade HIGH to LOW fails", func(t *testing.T) {
		_, err := ApplyEscalation(SeverityHigh, "LOW")
		if err == nil {
			t.Error("expected error for downgrade")
		}
	})

	t.Run("empty override returns original", func(t *testing.T) {
		got, err := ApplyEscalation(SeverityMedium, "")
		if err != nil {
			t.Fatal(err)
		}
		if got != SeverityMedium {
			t.Errorf("got %s, want MEDIUM", got)
		}
	})
}
