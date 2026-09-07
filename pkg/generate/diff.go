package generate

import (
	"fmt"
	"strings"
)

const diffContext = 3

type diffOp struct {
	kind byte // '=', '-', '+'
	line string
}

// unifiedDiff renders a unified diff between before and after, named name,
// with three lines of context. Identical contents produce "".
func unifiedDiff(name, before, after string) string {
	a, b := splitLines(before), splitLines(after)
	ops := diffOps(a, b)
	changed := false
	for _, o := range ops {
		if o.kind != '=' {
			changed = true
			break
		}
	}
	if !changed {
		return ""
	}

	type pos struct{ a, b int }
	positions := make([]pos, len(ops))
	ai, bi := 0, 0
	for k, o := range ops {
		positions[k] = pos{ai, bi}
		switch o.kind {
		case '=':
			ai++
			bi++
		case '-':
			ai++
		case '+':
			bi++
		}
	}

	var changes []int
	for k, o := range ops {
		if o.kind != '=' {
			changes = append(changes, k)
		}
	}

	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n", name, name)

	groups := [][]int{{changes[0]}}
	for _, c := range changes[1:] {
		if c-groups[len(groups)-1][len(groups[len(groups)-1])-1] <= 2*diffContext+1 {
			last := len(groups) - 1
			groups[last] = append(groups[last], c)
		} else {
			groups = append(groups, []int{c})
		}
	}

	for _, g := range groups {
		start := g[0] - diffContext
		if start < 0 {
			start = 0
		}
		end := g[len(g)-1] + diffContext + 1
		if end > len(ops) {
			end = len(ops)
		}
		aCount, bCount := 0, 0
		for k := start; k < end; k++ {
			switch ops[k].kind {
			case '=':
				aCount++
				bCount++
			case '-':
				aCount++
			case '+':
				bCount++
			}
		}
		aStart, bStart := positions[start].a+1, positions[start].b+1
		if aCount == 0 {
			aStart = positions[start].a
		}
		if bCount == 0 {
			bStart = positions[start].b
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount)
		for k := start; k < end; k++ {
			switch ops[k].kind {
			case '=':
				out.WriteString(" " + ops[k].line + "\n")
			case '-':
				out.WriteString("-" + ops[k].line + "\n")
			case '+':
				out.WriteString("+" + ops[k].line + "\n")
			}
		}
	}
	return out.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func diffOps(a, b []string) []diffOp {
	table := make([][]int, len(a)+1)
	for i := range table {
		table[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				table[i][j] = table[i+1][j+1] + 1
			case table[i+1][j] >= table[i][j+1]:
				table[i][j] = table[i+1][j]
			default:
				table[i][j] = table[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{'=', a[i]})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}
