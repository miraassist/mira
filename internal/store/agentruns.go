package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// AgentRun is the record of an agent's asynchronous run (docs/spec/agenten.md,
// The run record): what was called, how it stands, what came out.
type AgentRun struct {
	CallID   string
	ParentID string
	Agent    string
	// Status: running, done, failed, expired, cancelled.
	Status string
	// Call is the call context as the runtime stores it.
	Call   json.RawMessage
	Input  string
	Output string
	Error  string
	// Deadline is when the run expires; EndedAt is zero while it runs.
	StartedAt time.Time
	Deadline  time.Time
	EndedAt   time.Time
}

// AgentStep is one part a run's round added: a model answer, a tool marked
// as started, a tool result.
type AgentStep struct {
	Seq  int
	Kind string
	Body json.RawMessage
}

// PutAgentRun writes a run record as it starts.
func (s *Store) PutAgentRun(ctx context.Context, r AgentRun) error {
	if r.CallID == "" || r.Agent == "" || r.Status == "" {
		return errors.New("run needs call id, agent and status")
	}
	call := r.Call
	if len(call) == 0 {
		call = json.RawMessage("{}")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO agent_runs(call_id, parent_id, agent, status, call, input, output, error, started_at, deadline, ended_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.CallID, r.ParentID, r.Agent, r.Status, string(call), r.Input, r.Output, r.Error,
		formatTime(r.StartedAt), formatTime(r.Deadline), formatTime(r.EndedAt))
	return err
}

// EndAgentRun gives a run its final status, its output or error.
func (s *Store) EndAgentRun(ctx context.Context, callID, status, output, errText string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE agent_runs SET status = ?, output = ?, error = ?, ended_at = ? WHERE call_id = ?`,
		status, output, errText, formatTime(s.now()), callID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const agentRunSelect = `SELECT call_id, parent_id, agent, status, call, input, output, error, started_at, deadline, ended_at FROM agent_runs`

func scanAgentRun(row scannable) (AgentRun, error) {
	var r AgentRun
	var call, started, deadline, ended string
	if err := row.Scan(&r.CallID, &r.ParentID, &r.Agent, &r.Status, &call, &r.Input, &r.Output, &r.Error,
		&started, &deadline, &ended); err != nil {
		return AgentRun{}, err
	}
	r.Call = json.RawMessage(call)
	r.StartedAt, r.Deadline, r.EndedAt = parseTime(started), parseTime(deadline), parseTime(ended)
	return r, nil
}

func (s *Store) GetAgentRun(ctx context.Context, callID string) (AgentRun, error) {
	r, err := scanAgentRun(s.db.QueryRowContext(ctx, agentRunSelect+` WHERE call_id = ?`, callID))
	if err == sql.ErrNoRows {
		return AgentRun{}, ErrNotFound
	}
	return r, err
}

// RunningAgentRuns are the runs still running, oldest first: after a
// restart they are continued.
func (s *Store) RunningAgentRuns(ctx context.Context) ([]AgentRun, error) {
	rows, err := s.db.QueryContext(ctx, agentRunSelect+` WHERE status = 'running' ORDER BY started_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentRun
	for rows.Next() {
		r, err := scanAgentRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddAgentStep appends a part to a run's history.
func (s *Store) AddAgentStep(ctx context.Context, callID, kind string, body json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO agent_run_steps(call_id, seq, kind, body)
VALUES (?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM agent_run_steps WHERE call_id = ?), ?, ?)`,
		callID, callID, kind, string(body))
	return err
}

// AgentSteps is a run's history, in order.
func (s *Store) AgentSteps(ctx context.Context, callID string) ([]AgentStep, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq, kind, body FROM agent_run_steps WHERE call_id = ? ORDER BY seq`, callID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentStep
	for rows.Next() {
		var st AgentStep
		var body string
		if err := rows.Scan(&st.Seq, &st.Kind, &body); err != nil {
			return nil, err
		}
		st.Body = json.RawMessage(body)
		out = append(out, st)
	}
	return out, rows.Err()
}

// DeleteAgentRunsEndedBefore removes the runs that ended before t, with
// their history; it returns how many.
func (s *Store) DeleteAgentRunsEndedBefore(ctx context.Context, t time.Time) (int, error) {
	cut := formatTime(t)
	if _, err := s.db.ExecContext(ctx, `
DELETE FROM agent_run_steps WHERE call_id IN (SELECT call_id FROM agent_runs WHERE ended_at != '' AND ended_at < ?)`, cut); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM agent_runs WHERE ended_at != '' AND ended_at < ?`, cut)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
