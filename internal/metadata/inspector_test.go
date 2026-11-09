package metadata

import "testing"

func TestInspect_ListHiddenFromMetaTag(t *testing.T) {
	type sample struct {
		Visible string `json:"visible" meta:"label:Visible"`
		Secret  string `json:"secret" meta:"label:Secret,list:false"`
	}

	def := Inspect(sample{}, "Sample", TypeCatalog)

	hidden := map[string]bool{}
	labels := map[string]string{}
	for _, f := range def.Fields {
		hidden[f.Name] = f.ListHidden
		labels[f.Name] = f.Label
	}

	if hidden["visible"] {
		t.Fatalf("visible field should not be list-hidden")
	}
	if !hidden["secret"] {
		t.Fatalf("secret field should be list-hidden")
	}
	if labels["secret"] != "Secret" {
		t.Fatalf("list:false must keep the form label, got %q", labels["secret"])
	}
}

func TestInspect_WriteOnlyFieldWithIgnoredJSON(t *testing.T) {
	type sample struct {
		Name   string `json:"name" meta:"label:Name"`
		Secret string `json:"-" meta:"name:secret,label:Secret,writeOnly:true,list:false"`
	}

	def := Inspect(sample{}, "Sample", TypeCatalog)
	if len(def.Fields) != 2 {
		t.Fatalf("fields = %d, want 2", len(def.Fields))
	}
	secret := def.Fields[1]
	if secret.Name != "secret" || !secret.WriteOnly || !secret.ListHidden {
		t.Fatalf("write-only metadata = %+v", secret)
	}
	for _, field := range def.ToFilterMeta(nil) {
		if field.Key == "secret" {
			t.Fatal("write-only field must not be available to filters")
		}
	}
}

func TestInspect_MapAttributesAreJSON(t *testing.T) {
	type sample struct {
		Attributes map[string]any `json:"attributes" meta:"label:Attributes"`
	}
	def := Inspect(sample{}, "Sample", TypeDocument)
	if len(def.Fields) != 1 || def.Fields[0].Type != TypeJSON {
		t.Fatalf("attributes field metadata = %+v, want JSON", def.Fields)
	}
}

func TestInspect_ExplicitMetaType(t *testing.T) {
	type sample struct {
		RequestedAmount int64 `json:"requestedAmount" meta:"label:Запрошено,type:integer"`
	}
	def := Inspect(sample{}, "Sample", TypeDocument)
	if len(def.Fields) != 1 || def.Fields[0].Type != TypeInteger {
		t.Fatalf("requestedAmount field metadata = %+v, want TypeInteger", def.Fields)
	}
}

func TestInspect_ReadOnlyTagKeepsCustodyReferenceImmutableInForm(t *testing.T) {
	type sample struct {
		Owner      string `json:"owner" meta:"label:Владелец,ref:merchant,readOnly:true"`
		Assignment string `json:"assignment" meta:"ref:merchant"`
	}
	def := Inspect(sample{}, "Sample", TypeCatalog)
	if !def.Fields[0].ReadOnly || def.Fields[1].ReadOnly {
		t.Fatalf("read-only fields: %+v", def.Fields)
	}
}
