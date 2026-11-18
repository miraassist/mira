package tools

// ToolKeepListening asks the satellite to listen again after the answer,
// without a question at its end (docs/spec/satellite.md). The engine sees the
// call and sets continue_conversation; the tool itself is in
// internal/satellite.
const ToolKeepListening = "keep_listening"

// ToolStaySilent ends a voice turn at a satellite without any answer, when
// the user only wants quiet (docs/spec/streaming.md, Silence when asked for
// it). The engine sees the call; the tool itself is in internal/satellite.
const ToolStaySilent = "stay_silent"
