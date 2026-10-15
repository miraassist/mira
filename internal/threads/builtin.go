package threads

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
)

// MemorySave is the building block "memory.save": the field "fact" goes
// to the household memory, at once, without a yes.
func MemorySave(st *store.Store, lang i18n.Lang) Finisher { return memorySave{st, lang} }

type memorySave struct {
	st   *store.Store
	lang i18n.Lang
}

func (memorySave) Async() bool { return false }

func (f memorySave) Work(ctx context.Context, th store.Thread) (Result, error) {
	fact := strings.TrimSpace(th.Fields["fact"])
	if fact == "" {
		return Result{}, errors.New("the field fact is missing")
	}
	if _, err := f.st.SaveFact(ctx, store.Fact{ID: ulid.Make().String(), Text: fact, Source: "thread:" + th.ID}); err != nil {
		return Result{}, err
	}
	return Result{Done: fmt.Sprintf(i18n.Pick(catalog, f.lang).memorySaved, fact)}, nil
}

func (memorySave) Commit(context.Context, store.Thread) (string, error) {
	return "", errors.New("memory.save needs no yes")
}
