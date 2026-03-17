package tier

import "testing"

func TestLevel_String(t *testing.T) {
	tests := []struct {
		name string
		lvl  Level
		want string
	}{
		{"L1", L1, "L1:Active"},
		{"L2", L2, "L2:Hot"},
		{"L3", L3, "L3:Warm"},
		{"L4", L4, "L4:Semantic"},
		{"L5", L5, "L5:Cold"},
		{"Invalid_Zero", Level(0), "Unknown"},
		{"Invalid_Negative", Level(-1), "Unknown"},
		{"Invalid_High", Level(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.lvl.String()
			if got != tt.want {
				t.Errorf("Level(%d).String() = %q, want %q", int(tt.lvl), got, tt.want)
			}
		})
	}
}

func TestMemory_Defaults(t *testing.T) {
	var m Memory

	if m.Tier != 0 {
		t.Errorf("zero-value Memory.Tier = %d, want 0", m.Tier)
	}
	if m.Relevance != 0 {
		t.Errorf("zero-value Memory.Relevance = %f, want 0", m.Relevance)
	}
	if m.ID != "" {
		t.Errorf("zero-value Memory.ID = %q, want empty", m.ID)
	}
	if m.Content != "" {
		t.Errorf("zero-value Memory.Content = %q, want empty", m.Content)
	}
	if m.TokenCount != 0 {
		t.Errorf("zero-value Memory.TokenCount = %d, want 0", m.TokenCount)
	}
	if m.IsSummary {
		t.Error("zero-value Memory.IsSummary = true, want false")
	}
	if m.Keywords != nil {
		t.Errorf("zero-value Memory.Keywords = %v, want nil", m.Keywords)
	}
	if m.PromotedFrom != 0 {
		t.Errorf("zero-value Memory.PromotedFrom = %d, want 0", m.PromotedFrom)
	}
	if m.CreatedAt.IsZero() == false {
		t.Error("zero-value Memory.CreatedAt should be zero time")
	}
	if m.LastAccessedAt.IsZero() == false {
		t.Error("zero-value Memory.LastAccessedAt should be zero time")
	}
}
