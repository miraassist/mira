package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ToolCalculate computes an arithmetic expression exactly: models count and
// add badly once there are more than a few numbers.
const ToolCalculate = "calculate"

func calculateTool(t texts, decode func(json.RawMessage, any) error) InternalTool {
	return InternalTool{
		Name:        ToolCalculate,
		Description: t.calculateDesc,
		Schema:      schema(`{"type":"object","properties":{"expression":{"type":"string","description":%q}},"required":["expression"]}`, t.calculateExpr),
		ReadOnly:    true,
		Run: func(_ context.Context, inv Invocation) (string, error) {
			var a struct {
				Expression string `json:"expression"`
			}
			if err := decode(inv.Args, &a); err != nil {
				return "", err
			}
			v, err := Calculate(a.Expression)
			if err != nil {
				return "", fmt.Errorf(t.calculateBad, err)
			}
			return v, nil
		},
	}
}

// Calculate evaluates numbers with a decimal point, + - * / and brackets,
// exactly (as fractions), and returns the result with at most six decimals.
func Calculate(expr string) (string, error) {
	p := &calc{s: strings.ReplaceAll(expr, " ", "")}
	if p.s == "" {
		return "", errors.New("empty expression")
	}
	v, err := p.sum()
	if err != nil {
		return "", err
	}
	if p.i < len(p.s) {
		return "", fmt.Errorf("unexpected %q at position %d", p.s[p.i], p.i+1)
	}
	out := v.FloatString(6)
	if strings.Contains(out, ".") {
		out = strings.TrimRight(strings.TrimRight(out, "0"), ".")
	}
	if out == "-0" {
		out = "0"
	}
	return out, nil
}

// calc is a recursive descent over the expression.
type calc struct {
	s string
	i int
}

func (p *calc) sum() (*big.Rat, error) {
	v, err := p.product()
	if err != nil {
		return nil, err
	}
	for p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
		op := p.s[p.i]
		p.i++
		w, err := p.product()
		if err != nil {
			return nil, err
		}
		if op == '+' {
			v.Add(v, w)
		} else {
			v.Sub(v, w)
		}
	}
	return v, nil
}

func (p *calc) product() (*big.Rat, error) {
	v, err := p.factor()
	if err != nil {
		return nil, err
	}
	for p.i < len(p.s) && (p.s[p.i] == '*' || p.s[p.i] == '/') {
		op := p.s[p.i]
		p.i++
		w, err := p.factor()
		if err != nil {
			return nil, err
		}
		if op == '*' {
			v.Mul(v, w)
		} else {
			if w.Sign() == 0 {
				return nil, errors.New("division by zero")
			}
			v.Quo(v, w)
		}
	}
	return v, nil
}

func (p *calc) factor() (*big.Rat, error) {
	if p.i >= len(p.s) {
		return nil, errors.New("the expression ends too early")
	}
	switch c := p.s[p.i]; {
	case c == '-':
		p.i++
		v, err := p.factor()
		if err != nil {
			return nil, err
		}
		return v.Neg(v), nil
	case c == '(':
		p.i++
		v, err := p.sum()
		if err != nil {
			return nil, err
		}
		if p.i >= len(p.s) || p.s[p.i] != ')' {
			return nil, errors.New("a closing bracket is missing")
		}
		p.i++
		return v, nil
	case c >= '0' && c <= '9' || c == '.':
		start := p.i
		for p.i < len(p.s) && (p.s[p.i] >= '0' && p.s[p.i] <= '9' || p.s[p.i] == '.') {
			p.i++
		}
		v, ok := new(big.Rat).SetString(p.s[start:p.i])
		if !ok {
			return nil, fmt.Errorf("%q is not a number", p.s[start:p.i])
		}
		return v, nil
	default:
		return nil, fmt.Errorf("unexpected %q at position %d (numbers with a decimal point, + - * / and brackets)", c, p.i+1)
	}
}
