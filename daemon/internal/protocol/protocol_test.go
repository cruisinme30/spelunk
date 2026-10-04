package protocol

import (
	"encoding/json"
	"testing"
)

func TestUnionMarshalEmitsOnlyVariantFields(t *testing.T) {
	n := Node{Kind: "and", Span: Span{0, 5}, Children: []Node{
		{Kind: "text", Value: "a", Match: MatchLiteral, Span: Span{0, 1}},
		{Kind: "not", Span: Span{2, 5}, Child: &Node{Kind: "op", Op: OpNameF, Value: "x", Match: MatchRegex, Span: Span{3, 5}}},
	}}
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"children":[{"kind":"text","match":"literal","span":{"start":0,"end":1},"termIndex":0,"value":"a"},{"child":{"kind":"op","match":"regex","op":"f","span":{"start":3,"end":5},"value":"x"},"kind":"not","span":{"start":2,"end":5}}],"kind":"and","span":{"start":0,"end":5}}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestRequiredArraysMarshalAsEmpty(t *testing.T) {
	b, _ := json.Marshal(SearchResult{})
	if string(b) != `{"total":0,"truncated":false,"hidden":[],"ms":0}` {
		t.Fatalf("got %s", b)
	}
	b, _ = json.Marshal(ResultItem{Kind: "line", Path: "a.go", Line: 3})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["hits"].([]any); !ok {
		t.Fatalf("hits should be [] in %s", b)
	}
	if _, ok := m["nameHits"]; ok {
		t.Fatalf("file-only field leaked into line item: %s", b)
	}
}
