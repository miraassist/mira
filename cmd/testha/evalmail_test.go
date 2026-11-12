package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	go func() {
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		f.WriteString("msg=other key=A\nmsg=do")
		time.Sleep(50 * time.Millisecond)
		f.WriteString("ne key=A\n")
		f.Close()
	}()
	var seen []string
	err := waitLog(context.Background(), path, from, 5*time.Second, func(line string) (bool, error) {
		seen = append(seen, strings.TrimSpace(line))
		return strings.Contains(line, "msg=done"), nil
	})
	if err != nil || len(seen) != 2 || seen[1] != "msg=done key=A" {
		t.Fatalf("err %v, seen %q", err, seen)
	}
	if err := waitLog(context.Background(), path, from, 300*time.Millisecond, func(string) (bool, error) { return false, nil }); err == nil {
		t.Fatal("no end without the line")
	}
}
