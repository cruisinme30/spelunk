package query

import "testing"

// TestFixesOnMutatedKernelQueries exercises repairs around paths and symbols
// from kernel/fork.c and mm/memory.c, with a single edit at every position.
func TestFixesOnMutatedKernelQueries(t *testing.T) {
	seeds := []string{
		`sym:copy_process f:kernel/fork.c`,
		`(handle_mm_fault OR do_anonymous_page) f:mm/memory.c`,
		`f:drivers/net/ethernet/ -sym:dup_task_struct case:yes`,
		`"copy_process" -content:CLONE_VM lang:c`,
		`msg:"mm: memory" since:2y type:commit`,
		`unknown:"drivers\\net" handle_mm_fault`,
		`(case:yes (count:200 "copy_process")) OR kernel_clone`,
	}
	mutations := []string{"", `(`, `)`, `"`, `/`, `\`, ` OR `, `AND `, `-`, `case:yes`, ` unknown:\ `}
	checked := 0
	for _, seed := range seeds {
		for offset := range len(seed) + 1 {
			for _, mutation := range mutations {
				text := seed[:offset] + mutation + seed[min(offset+1, len(seed)):]
				src := newSource(text)
				q := Parse(text, testResolver)
				for _, d := range q.Diagnostics {
					for _, fix := range d.Fixes {
						checkFix(t, src, text, d, fix)
					}
				}
				checked++
			}
		}
	}
	t.Logf("checked %d mutated kernel queries", checked)
}
