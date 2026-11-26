package config

import "testing"

func TestWhaleSavedMainRef(t *testing.T) {
	c := Config{Providers: []ProviderEntry{{Name: "鲸栈", Model: "lens-main", APIKeyEnv: "WHALESTACK_API_KEY"}, {Name: "custom", Model: "lens-main"}}}
	if e, ok := c.ResolveModel("鲸栈/Chimera_v1"); !ok || e.Model != "lens-main" || e.Name != "鲸栈" {
		t.Fatal("lost old local selection")
	}
	if _, ok := c.ResolveModel("custom/Chimera_v1"); ok {
		t.Fatal("rewrote another provider")
	}
}
