package protocol

import (
	"encoding/json"
	"testing"

	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
)

func TestUnionMarshalsOnlyItsVariantsFields(t *testing.T) {
	node := Node{Kind: "and", Span: Span{Start: 0, End: 5}, Children: []Node{
		{Kind: "text", Value: "a", Match: MatchLiteral, Span: Span{Start: 0, End: 1}},
		{Kind: "not", Span: Span{Start: 2, End: 5}, Child: &Node{Kind: "op", Op: OpNameF, Value: "x", Match: MatchRegex, Span: Span{Start: 3, End: 5}}},
	}}
	got, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"children":[{"kind":"text","match":"literal","span":{"start":0,"end":1},"termIndex":0,"value":"a"},{"child":{"kind":"op","match":"regex","op":"f","span":{"start":3,"end":5},"value":"x"},"kind":"not","span":{"start":2,"end":5}}],"kind":"and","span":{"start":0,"end":5}}`
	if string(got) != want {
		t.Fatalf("json.Marshal(node) =\n%s\nwant\n%s", got, want)
	}
}

func TestRequiredArraysMarshalAsEmptyNotNull(t *testing.T) {
	got, _ := json.Marshal(SearchResult{})
	if want := `{"total":0,"truncated":false,"hidden":[],"ms":0}`; string(got) != want {
		t.Fatalf("json.Marshal(SearchResult{}) = %s, want %s", got, want)
	}
	got, _ = json.Marshal(ResultItem{Kind: "line", Path: "a.go", Line: 3})
	var fields map[string]any
	_ = json.Unmarshal(got, &fields)
	if _, ok := fields["hits"].([]any); !ok {
		t.Fatalf("line item %s: hits missing or null, want []", got)
	}
	if _, ok := fields["nameHits"]; ok {
		t.Fatalf("line item %s: has the file-only field nameHits, want it absent", got)
	}
}

func TestRequestCancelledMatchesTheTransportCode(t *testing.T) {
	if CodeRequestCancelled != rpc.CodeRequestCancelled {
		t.Fatalf("CodeRequestCancelled = %d, want rpc.CodeRequestCancelled (%d)", CodeRequestCancelled, rpc.CodeRequestCancelled)
	}
}
