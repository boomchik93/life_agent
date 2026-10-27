package llm

import "testing"

func str(s string) *string { return &s }

func TestDefaultConfig(t *testing.T) {
	c, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider.Name != "openrouter" || c.Limits.MaxCallsPerRequest != 4 || c.Limits.TimeoutSec != 60 {
		t.Errorf("%+v", c)
	}
	if c.Tasks[TaskParse].Effort != "low" || c.Tasks[TaskAdvice].Effort != "medium" {
		t.Errorf("default efforts of section 14: %+v", c.Tasks)
	}
	// A fresh server parses phrases without a trip to the settings: the
	// default is the model chosen on the reference set (stage 5).
	if c.Tasks[TaskParse].Model == "" || c.Tasks[TaskAdvice].Model == "" {
		t.Errorf("default models must be set: %+v", c.Tasks)
	}
}

func TestParseOverrides(t *testing.T) {
	good := `{"parse":{"model":"a/m:free","effort":"high","fallbacks":["b/m:free","c/m:free"]},"advice":{"effort":"low"}}`
	m, err := ParseOverrides([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if *m[TaskParse].Model != "a/m:free" || len(*m[TaskParse].Fallbacks) != 2 || m[TaskAdvice].Model != nil {
		t.Errorf("%+v", m)
	}
	for _, bad := range []string{
		`[]`, `{"nope":{}}`, `{"parse":{"effort":"max"}}`, `{"parse":{"model":"has space/x"}}`,
		`{"parse":{"model":"nomodelslash"}}`, `{"parse":{"fallbacks":["a/1","b/2","c/3","d/4"]}}`,
		`{"parse":{"fallbacks":["a/1","a/1"]}}`, `{"parse":{"model":"a/1","fallbacks":["a/1"]}}`,
		`{"parse":{"fallbacks":[""]}}`, `{"parse":{"model":5}}`,
	} {
		if _, err := ParseOverrides([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestMerge(t *testing.T) {
	base, _ := DefaultConfig()
	over := map[Task]Override{TaskParse: {Model: str("a/m:free"), Fallbacks: &[]string{"b/m:free"}}}
	c, err := base.Merge(over)
	if err != nil {
		t.Fatal(err)
	}
	p := c.Tasks[TaskParse]
	if p.Model != "a/m:free" || p.Effort != "low" || p.MaxTokens != 2000 || len(p.Fallbacks) != 1 {
		t.Errorf("override on top of defaults: %+v", p)
	}
	if c.Tasks[TaskAdvice].Model != base.Tasks[TaskAdvice].Model {
		t.Error("untouched task keeps its default")
	}
	if fresh, _ := DefaultConfig(); base.Tasks[TaskParse].Model != fresh.Tasks[TaskParse].Model {
		t.Error("Merge must not modify the defaults")
	}

	// A new main model equal to a stored fallback breaks the chain: reported, not applied silently.
	if _, err := c.Merge(map[Task]Override{TaskParse: {Model: str("b/m:free")}}); err == nil {
		t.Error("main model equal to a stored fallback accepted")
	}
	if _, err := base.Merge(map[Task]Override{TaskParse: {Model: str("b/m:free")}}); err != nil {
		t.Fatalf("no fallbacks in the defaults: %v", err)
	}
	both := map[Task]Override{TaskParse: {Model: str("b/m:free"), Fallbacks: &[]string{"b/m:free"}}}
	if _, err := base.Merge(both); err == nil {
		t.Error("duplicate model in the merged chain accepted")
	}
}
