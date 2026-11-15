package tools

import "context"

type personKey struct{}

// WithPerson returns ctx carrying whose thread or turn it is, for a check
// that decides by the person (the calendar's one rule,
// docs/spec/calendar.md, Adding).
func WithPerson(ctx context.Context, person string) context.Context {
	return context.WithValue(ctx, personKey{}, person)
}

// PersonFrom is the person ctx carries; "" for none.
func PersonFrom(ctx context.Context) string {
	p, _ := ctx.Value(personKey{}).(string)
	return p
}
