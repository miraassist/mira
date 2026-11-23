package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
)

func TestFillDaysNamesTheGermanDate(t *testing.T) {
	d := time.Now().AddDate(0, 0, 7)
	got := fillDays("am {{+7}} um 19:30")
	want := "am " + weekdaysDE[d.Weekday()] + ", "
	if !strings.HasPrefix(got, want) || !strings.Contains(got, monthsDE[d.Month()-1]) || strings.Contains(got, "{{") {
		t.Fatalf("got %q", got)
	}
}

func TestWaitLogReadsOnlyWhatCameAfter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	if err := os.WriteFile(path, []byte("msg=done key=A\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	from, _ := logSize(path)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	defer f.Close()
	f.WriteString("msg=other key=A\nmsg=do") // a line half written
	clk := clock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))
	var seen []string
	found := make(chan error, 1)
	go func() {
		found <- waitLog(context.Background(), clk, path, from, 5*time.Second, func(line string) (bool, error) {
			seen = append(seen, strings.TrimSpace(line))
			return strings.Contains(line, "msg=done"), nil
		})
	}()
	<-clk.AwaitWaiters(1) // it read what is there and waits for more
	f.WriteString("ne key=A\n")
	clk.Advance(200 * time.Millisecond)
	err := <-found
	if err != nil || len(seen) != 2 || seen[1] != "msg=done key=A" {
		t.Fatalf("err %v, seen %q", err, seen)
	}
	var none error
	clk.Drive(func() {
		none = waitLog(context.Background(), clk, path, from, 300*time.Millisecond, func(string) (bool, error) { return false, nil })
	})
	if none == nil {
		t.Fatal("no end without the line")
	}
}
